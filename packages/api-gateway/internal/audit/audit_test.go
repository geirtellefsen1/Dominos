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

// Regression for the Phase 6 DB-round-trip bug. Postgres TIMESTAMPTZ is
// microsecond-precision; if an event is signed at nanosecond precision
// and then the Timestamp is later truncated (as happens when Query()
// reads it back from Postgres), canonicalBytes() produces a different
// string and Verify fails systematically. Store.Insert now truncates
// the Timestamp to microseconds BEFORE signing; this test asserts the
// invariant holds both before and after the round-trip.
func TestMicrosecondRoundTripPreservesSignature(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)

	e := &Event{
		ID:        uuid.New(),
		Timestamp: time.Unix(0, 1_234_567_890_123_456_789).UTC().Truncate(time.Microsecond),
		Actor:     "user:alice",
		Action:    "document.read",
		Decision:  "allow",
		KeyID:     "deadbeef",
	}
	if err := e.Sign(priv); err != nil {
		t.Fatal(err)
	}
	// Simulate the Postgres round-trip: already microsecond-precision, so
	// this truncation is a no-op — but we do it explicitly to mirror what
	// pgx will return on SELECT.
	e.Timestamp = e.Timestamp.Truncate(time.Microsecond)
	if err := e.Verify(pub); err != nil {
		t.Fatalf("verify after microsecond round-trip failed: %v", err)
	}
}

// Documents the bug we just fixed: if we skip the truncation step and
// sign at nanosecond precision, then truncate to simulate DB storage,
// verification MUST fail. If this test starts passing, the pre-sign
// truncation has been removed and Phase 4's acceptance test will break.
func TestNanosecondPrecisionFailsAfterTruncation(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)

	e := &Event{
		ID:        uuid.New(),
		Timestamp: time.Unix(0, 1_234_567_890_123_456_789).UTC(), // ns precision
		Actor:     "user:alice",
		Action:    "document.read",
		Decision:  "allow",
		KeyID:     "deadbeef",
	}
	if err := e.Sign(priv); err != nil {
		t.Fatal(err)
	}
	e.Timestamp = e.Timestamp.Truncate(time.Microsecond)
	if err := e.Verify(pub); err == nil {
		t.Fatal("verify should fail when ns-precision timestamp is truncated after signing — pre-sign truncation is missing")
	}
}
