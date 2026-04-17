package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := &Event{
		ID:        uuid.New(),
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "user:alice",
		Action:    "document.read",
		Resource:  "document:" + uuid.New().String(),
		Decision:  "allow",
		KeyID:     "deadbeef",
	}
	if err := e.Sign(priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if e.Signature == "" {
		t.Fatal("signature is empty after Sign")
	}
	if err := e.Verify(pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestTamperDetected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	e := &Event{
		ID:        uuid.New(),
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "user:alice",
		Action:    "document.read",
		Decision:  "allow",
		KeyID:     "deadbeef",
	}
	_ = e.Sign(priv)

	// Mutate a byte — verification must fail.
	e.Decision = "deny"
	if err := e.Verify(pub); err == nil {
		t.Fatal("expected verification to fail after tamper")
	}
}

func TestWrongKeyFails(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	e := &Event{
		ID: uuid.New(), Timestamp: time.Now().UTC(), Actor: "x",
		Action: "y", Decision: "allow", KeyID: "deadbeef",
	}
	_ = e.Sign(priv)
	if err := e.Verify(otherPub); err == nil {
		t.Fatal("expected verification with wrong public key to fail")
	}
}
