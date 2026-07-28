package capability

import (
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"testing"
	"time"
)

func TestCapabilityScopeAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	priv, pub, err := operatorapproval.Generate("operator", now)
	if err != nil {
		t.Fatal(err)
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	tok, err := Sign(priv, 1000, "transaction_commit", "tx-1", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, tok, 1000, "transaction_commit", "tx-1", now); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, tok, 1001, "transaction_commit", "tx-1", now); err == nil {
		t.Fatal("expected uid mismatch")
	}
	if err := Verify(ring, tok, 1000, "transaction_commit", "tx-1", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expected expiry")
	}
	compact, _ := EncodeCompact(tok)
	decoded, err := DecodeCompact(compact)
	if err != nil || decoded.CapabilityID != tok.CapabilityID {
		t.Fatal("compact roundtrip")
	}
}
