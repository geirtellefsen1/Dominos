package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// Keys holds the gateway's Ed25519 signing material. The public half is
// exposed via GET /admin/audit/key so a compliance officer can verify
// exported bundles offline.
type Keys struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
	// KeyID is the first 8 bytes of the pubkey, hex-encoded. Stored on
	// every audit row so a verifier can cope with a future key rotation.
	KeyID string
}

// LoadKeys reads DOMINION_AUDIT_PRIVATE_KEY (base64-encoded 64-byte
// Ed25519 seed+public, the format produced by ed25519.NewKeyFromSeed).
// If absent, a fresh keypair is generated and logged with a warning —
// only acceptable in dev, because exported bundles won't verify after a
// restart.
func LoadKeys() (*Keys, error) {
	if v := os.Getenv("DOMINION_AUDIT_PRIVATE_KEY"); v != "" {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("decode audit key: %w", err)
		}
		// Accept either the 32-byte seed or the 64-byte private key form.
		var priv ed25519.PrivateKey
		switch len(raw) {
		case ed25519.SeedSize:
			priv = ed25519.NewKeyFromSeed(raw)
		case ed25519.PrivateKeySize:
			priv = ed25519.PrivateKey(raw)
		default:
			return nil, errors.New("audit key must be 32 (seed) or 64 (private) bytes")
		}
		pub := priv.Public().(ed25519.PublicKey)
		return buildKeys(priv, pub), nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate audit key: %w", err)
	}
	slog.Warn("DOMINION_AUDIT_PRIVATE_KEY not set; generated ephemeral Ed25519 key (exported bundles will not verify across restarts)",
		"key_id", shortKeyID(pub))
	return buildKeys(priv, pub), nil
}

func buildKeys(priv ed25519.PrivateKey, pub ed25519.PublicKey) *Keys {
	return &Keys{Private: priv, Public: pub, KeyID: shortKeyID(pub)}
}

func shortKeyID(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub[:8])
}

// PublicKeyBase64 returns the pubkey for the /admin/audit/key endpoint.
func (k *Keys) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(k.Public)
}
