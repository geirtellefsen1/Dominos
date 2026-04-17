// Package cryptokeys provides a small AES-256-GCM helper for encrypting
// secrets stored in Postgres (OAuth refresh tokens, etc). The key is
// loaded from DOMINION_ENCRYPTION_KEY (base64, 32 bytes) or generated on
// boot with a loud warning.
package cryptokeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

type AESGCM struct {
	aead cipher.AEAD
}

func Load() (*AESGCM, error) {
	var raw []byte
	if v := os.Getenv("DOMINION_ENCRYPTION_KEY"); v != "" {
		k, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("decode DOMINION_ENCRYPTION_KEY: %w", err)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("DOMINION_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(k))
		}
		raw = k
	} else {
		raw = make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		slog.Warn("DOMINION_ENCRYPTION_KEY not set; generated ephemeral AES-GCM key (connector refresh tokens won't decrypt after restart)")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

// Seal returns nonce || ciphertext.
func (a *AESGCM) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (a *AESGCM) Open(blob []byte) ([]byte, error) {
	n := a.aead.NonceSize()
	if len(blob) < n {
		return nil, errors.New("ciphertext too short")
	}
	return a.aead.Open(nil, blob[:n], blob[n:], nil)
}
