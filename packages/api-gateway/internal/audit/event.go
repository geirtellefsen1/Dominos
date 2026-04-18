package audit

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event matches the shape in spec §3.7.
type Event struct {
	ID          uuid.UUID       `json:"id"`
	Timestamp   time.Time       `json:"timestamp"`
	Actor       string          `json:"actor"`
	OnBehalfOf  string          `json:"on_behalf_of,omitempty"`
	Action      string          `json:"action"`
	Resource    string          `json:"resource,omitempty"`
	Decision    string          `json:"decision"`
	Context     json.RawMessage `json:"context,omitempty"`
	KeyID       string          `json:"key_id"`
	Signature   string          `json:"signature,omitempty"`
}

// canonicalBytes returns a deterministic JSON encoding of the event with
// the `signature` field omitted. The form is:
//
//   - all object keys sorted lexicographically (Go's json.Marshal does
//     this for map[string]any; we decode Context so its nested keys also
//     get sorted),
//   - timestamp rendered as RFC3339Nano in UTC,
//   - HTML escaping disabled so `<>&` in fields like user-agents stay
//     literal (matches Python's json.dumps default),
//   - no trailing newline.
//
// A verifier reproduces this form via json.dumps with sort_keys=True,
// separators=(",", ":"), ensure_ascii=False.
func (e *Event) canonicalBytes() ([]byte, error) {
	m := map[string]any{
		"id":        e.ID.String(),
		"timestamp": e.Timestamp.UTC().Format(time.RFC3339Nano),
		"actor":     e.Actor,
		"action":    e.Action,
		"decision":  e.Decision,
		"key_id":    e.KeyID,
	}
	if e.OnBehalfOf != "" {
		m["on_behalf_of"] = e.OnBehalfOf
	}
	if e.Resource != "" {
		m["resource"] = e.Resource
	}
	if len(e.Context) > 0 {
		// Decode with UseNumber so large integers don't silently
		// round-trip through float64 (which loses precision above
		// 2^53 and re-emits integer-valued values like 3000 as
		// "3000" or "3e+03" depending on magnitude). The Python
		// verifier's json.dumps(sort_keys=True) preserves int shape
		// exactly — we need the Go signer to match.
		dec := json.NewDecoder(bytes.NewReader(e.Context))
		dec.UseNumber()
		var ctx any
		if err := dec.Decode(&ctx); err != nil {
			return nil, fmt.Errorf("decode context: %w", err)
		}
		m["context"] = ctx
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Sign fills in e.Signature using the provided private key.
func (e *Event) Sign(priv ed25519.PrivateKey) error {
	body, err := e.canonicalBytes()
	if err != nil {
		return err
	}
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	return nil
}

// Verify checks the signature against the public key.
func (e *Event) Verify(pub ed25519.PublicKey) error {
	if e.Signature == "" {
		return errors.New("empty signature")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil {
		return err
	}
	body, err := e.canonicalBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, body, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
