package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/agents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/audit"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/ca"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/cryptokeys"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/db"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/documents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/graph"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/llm"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/queue"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/scim"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/triage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("DOMINION_GATEWAY_ADDR", ":3000")
	tlsAddr := envOr("DOMINION_GATEWAY_TLS_ADDR", ":3443")
	dsn := envOr("DOMINION_DATABASE_URL",
		"postgres://dominion:dominion_dev@localhost:5432/dominion?sslmode=disable")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("db migrate", "err", err)
		os.Exit(1)
	}

	// --- auth / identity ---
	jwtSecret := loadSessionSecret()
	users := auth.NewUserStore(pool)
	sessions := auth.NewSessionStore(pool, jwtSecret)

	// --- audit ---
	auditKeys, err := audit.LoadKeys()
	if err != nil {
		slog.Error("audit keys", "err", err)
		os.Exit(1)
	}
	auditStore := audit.NewStore(pool, auditKeys)
	if err := auditStore.EnsurePartitions(ctx, 12); err != nil {
		slog.Error("audit partitions", "err", err)
		os.Exit(1)
	}

	// --- CA + agents (phase 5) ---
	authority, err := ca.Load()
	if err != nil {
		slog.Error("ca load", "err", err)
		os.Exit(1)
	}
	agentStore := agents.NewStore(pool)

	// --- ACL (OpenFGA) ---
	var fgaClient *acl.Client
	if url := os.Getenv("DOMINION_FGA_API_URL"); url != "" {
		fgaClient = acl.NewClient(url)
		bootstrapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := acl.Bootstrap(bootstrapCtx, fgaClient, envOr("DOMINION_FGA_STORE_NAME", "dominion")); err != nil {
			cancel()
			slog.Error("fga bootstrap", "err", err)
			os.Exit(1)
		}
		cancel()
	} else {
		slog.Warn("DOMINION_FGA_API_URL not set; running without ACL enforcement (dev only)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	if issuer := os.Getenv("DOMINION_OIDC_ISSUER"); issuer != "" {
		oidcClient, err := auth.NewOIDC(ctx, auth.OIDCConfig{
			IssuerURL:    issuer,
			ClientID:     os.Getenv("DOMINION_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("DOMINION_OIDC_CLIENT_SECRET"),
			RedirectURL:  envOr("DOMINION_OIDC_REDIRECT_URL", "http://localhost:3000/auth/callback"),
		}, users, sessions)
		if err != nil {
			slog.Error("oidc init", "err", err)
			os.Exit(1)
		}
		oidcClient.Register(mux)
	} else {
		slog.Warn("DOMINION_OIDC_ISSUER not set; /auth/login disabled until configured")
	}

	docStore := documents.NewStore(pool)
	validator := documents.NewValidator(docStore)
	documents.NewHandler(docStore, validator, fgaClient).Register(mux)

	mux.Handle("GET /me", auth.Require(auth.MeHandler(users)))

	scimStore := scim.NewStore(pool)
	scimToken := os.Getenv("DOMINION_SCIM_TOKEN")
	if scimToken == "" {
		slog.Warn("DOMINION_SCIM_TOKEN not set; SCIM endpoints will reject all requests")
	}
	scim.NewHandler(scimStore, sessions, scimToken).Register(mux)

	adminToken := os.Getenv("DOMINION_ADMIN_TOKEN")
	if adminToken == "" {
		slog.Warn("DOMINION_ADMIN_TOKEN not set; /admin/* endpoints will reject all requests")
	}
	if fgaClient != nil {
		acl.NewAdminHandler(fgaClient, adminToken).Register(mux)
	}
	audit.NewHandler(auditStore, adminToken).Register(mux)
	agents.NewHandler(agentStore, authority, adminToken).Register(mux)

	// --- Graph email connector (phase 6) ---
	encKey, err := cryptokeys.Load()
	if err != nil {
		slog.Error("crypto key", "err", err)
		os.Exit(1)
	}
	graphStore := graph.NewStore(pool, encKey)
	var graphClient *graph.Client
	if cid := os.Getenv("DOMINION_GRAPH_CLIENT_ID"); cid != "" {
		graphClient = graph.NewClient(graph.Config{
			TenantID:     envOr("DOMINION_GRAPH_TENANT_ID", "common"),
			ClientID:     cid,
			ClientSecret: os.Getenv("DOMINION_GRAPH_CLIENT_SECRET"),
			RedirectURL:  envOr("DOMINION_GRAPH_REDIRECT_URL", "http://localhost:3000/connectors/graph/callback"),
		})
	} else {
		slog.Warn("DOMINION_GRAPH_CLIENT_ID not set; /connectors/graph/* disabled (simulate still available if dev flag set)")
	}
	ingester := graph.NewIngester(pool, docStore, fgaClient)
	devSimulate := strings.EqualFold(os.Getenv("DOMINION_DEV_GRAPH_SIMULATE"), "true")
	if devSimulate {
		slog.Warn("DOMINION_DEV_GRAPH_SIMULATE=true — /admin/connectors/graph/simulate is exposed. Never enable in production.")
	}
	graph.NewHandler(graphStore, graphClient, ingester, adminToken, devSimulate).Register(mux)

	// Start the 60s poller only when we have credentials.
	if graphClient != nil {
		go graph.NewPoller(graphStore, graphClient, ingester).Run(ctx)
	}

	// --- Triage engine + approval queue (phase 7) ---
	llmClient := llm.New()
	if llmClient.IsStub() {
		slog.Warn("DOMINION_ANTHROPIC_API_KEY not set; triage runs in deterministic stub mode")
	}
	triageOpts := triage.Options{
		Interval: envDuration("DOMINION_TRIAGE_INTERVAL", 5*time.Minute),
		Lookback: envDuration("DOMINION_TRIAGE_LOOKBACK", time.Hour),
	}
	triageEngine := triage.NewEngine(pool, docStore, fgaClient, llmClient, triageOpts)
	triage.NewHandler(triageEngine, adminToken).Register(mux)
	go triageEngine.Run(ctx)

	simulateSend := strings.EqualFold(os.Getenv("DOMINION_DEV_SIMULATE_SEND"), "true") ||
		(graphClient == nil && strings.EqualFold(os.Getenv("DOMINION_DEV_GRAPH_SIMULATE"), "true"))
	if simulateSend {
		slog.Warn("approval queue will simulate sendMail instead of calling Graph (dev only)")
	}
	queue.NewHandler(pool, docStore, fgaClient, queue.Options{
		GraphStore:   graphStore,
		GraphClient:  graphClient,
		SimulateSend: simulateSend,
	}).Register(mux)

	devHeader := strings.EqualFold(os.Getenv("DOMINION_DEV_PRINCIPAL_HEADER"), "true")
	if devHeader {
		slog.Warn("DOMINION_DEV_PRINCIPAL_HEADER=true — X-Dominion-Dev-Principal header is trusted. Never enable in production.")
	}
	// Middleware order (outer → inner):
	//   audit  → the tap records every request that reaches the gateway
	//   attach → resolves Principal from cookie / client cert / dev header
	//   mux    → route handlers
	handler := audit.Middleware(auditStore)(
		auth.Attach(sessions, auth.AttachOptions{
			DevPrincipalHeader: devHeader,
			Agents:             agentStore,
		})(mux),
	)

	// --- HTTP listener (:3000) ---
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("dominion api gateway (http) listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "err", err)
			stop()
		}
	}()

	// --- TLS listener (:3443), accepts agent client certs ---
	var tlsSrv *http.Server
	if strings.EqualFold(os.Getenv("DOMINION_TLS_ENABLED"), "true") {
		tlsSrv, err = startTLS(tlsAddr, handler, authority)
		if err != nil {
			slog.Error("tls listener", "err", err)
			os.Exit(1)
		}
	} else {
		slog.Warn("DOMINION_TLS_ENABLED not true; mTLS agent listener disabled")
	}

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	if tlsSrv != nil {
		_ = tlsSrv.Shutdown(shutdownCtx)
	}
}

// startTLS issues a server cert from the internal CA (valid for localhost
// and any hostnames in DOMINION_TLS_HOSTNAMES) and starts an HTTPS
// listener that accepts optional client certs. Clients with a valid cert
// become agent principals; humans with a session cookie still work.
func startTLS(addr string, handler http.Handler, authority *ca.CA) (*http.Server, error) {
	hostnames := []string{}
	if v := os.Getenv("DOMINION_TLS_HOSTNAMES"); v != "" {
		for _, h := range strings.Split(v, ",") {
			hostnames = append(hostnames, strings.TrimSpace(h))
		}
	}
	ips := []net.IP{}
	if v := os.Getenv("DOMINION_TLS_IPS"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
				ips = append(ips, ip)
			}
		}
	}

	srvCert, err := authority.IssueServer(hostnames, ips)
	if err != nil {
		return nil, err
	}
	certBlock, _ := pem.Decode(srvCert.CertPEM)
	keyBlock, _ := pem.Decode(srvCert.KeyPEM)
	tlsCert := tls.Certificate{
		Certificate: [][]byte{certBlock.Bytes},
	}
	priv, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	tlsCert.PrivateKey = priv

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    authority.ClientCAPool(),
			MinVersion:   tls.VersionTLS12,
		},
	}
	go func() {
		slog.Info("dominion api gateway (tls) listening", "addr", addr, "mtls", "optional")
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("tls server failed", "err", err)
		}
	}()
	return srv, nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func loadSessionSecret() []byte {
	if s := os.Getenv("DOMINION_SESSION_SECRET"); s != "" {
		return []byte(s)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("cannot generate session secret: " + err.Error())
	}
	slog.Warn("DOMINION_SESSION_SECRET not set; generated ephemeral secret (sessions will not survive restart)",
		"secret_preview", base64.RawURLEncoding.EncodeToString(buf[:4])+"...")
	return buf
}
