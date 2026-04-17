// Command agent is the minimum viable Dominion agent runtime.
//
// Phase 5 scope: prove the governance spine (scoped mTLS identity,
// gateway-routed tools, full audit) with a standalone binary. The
// fork-OpenClaw-and-strip-it work from spec §3.5 lands incrementally
// as the higher phases need the missing features — for now every tool
// call already goes through the Dominion API gateway, which is the
// invariant that actually matters.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	certPath := getFlag("--cert")
	keyPath := getFlag("--key")
	caPath := getFlag("--ca")
	gateway := getFlag("--gateway")
	if certPath == "" || keyPath == "" || caPath == "" || gateway == "" {
		usage()
		os.Exit(2)
	}
	gateway = strings.TrimRight(gateway, "/")

	client, err := newClient(certPath, keyPath, caPath)
	if err != nil {
		log.Fatalf("agent: init: %v", err)
	}

	switch os.Args[1] {
	case "read":
		docID := requireArg(2)
		mustJSON(client, http.MethodGet, gateway+"/documents/"+docID, nil)
	case "list":
		schema := optionalFlag("--schema", "")
		q := ""
		if schema != "" {
			q = "?schema=" + schema
		}
		mustJSON(client, http.MethodGet, gateway+"/documents"+q, nil)
	case "write":
		schemaID := requireArg(2)
		bodyPath := requireArg(3)
		raw, err := os.ReadFile(bodyPath)
		if err != nil {
			log.Fatalf("agent: read body: %v", err)
		}
		payload := map[string]any{
			"schema_id": schemaID,
			"body":      json.RawMessage(raw),
		}
		buf, _ := json.Marshal(payload)
		mustJSON(client, http.MethodPost, gateway+"/documents", buf)
	case "whoami":
		// /me doesn't exist for agents; instead we echo the subject the
		// gateway sees by calling a harmless endpoint and letting the
		// audit log record the principal.
		mustJSON(client, http.MethodGet, gateway+"/health", nil)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: agent <command> [args] --cert <path> --key <path> --ca <path> --gateway <url>

commands:
  read <doc-uuid>
  list [--schema <schema-id>]
  write <schema-id> <body.json>
  whoami`)
}

func newClient(certPath, keyPath, caPath string) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load cert/key: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certs in %s", caPath)
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      pool,
				MinVersion:   tls.VersionTLS12,
			},
		},
	}, nil
}

func mustJSON(c *http.Client, method, url string, body []byte) {
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		log.Fatalf("agent: build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		log.Fatalf("agent: %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, string(buf))
	if resp.StatusCode >= 400 {
		os.Exit(resp.StatusCode / 100)
	}
}

// --- tiny flag parser -------------------------------------------------
// We want positional args before flags (e.g. `agent read <id> --cert ...`),
// which stdlib flag doesn't easily support. Keep it simple.

func getFlag(name string) string {
	for i := 2; i < len(os.Args)-1; i++ {
		if os.Args[i] == name {
			return os.Args[i+1]
		}
	}
	return ""
}

func optionalFlag(name, def string) string {
	v := getFlag(name)
	if v == "" {
		return def
	}
	return v
}

func requireArg(pos int) string {
	if pos >= len(os.Args) || strings.HasPrefix(os.Args[pos], "--") {
		log.Fatalf("agent: missing positional arg at position %d", pos)
	}
	return os.Args[pos]
}

// avoid unused-import warning in some build configurations
var _ = flag.CommandLine
