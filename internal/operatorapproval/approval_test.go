package operatorapproval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSignVerifyAndTamper(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	priv, pub, err := Generate("alice@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	ring := Keyring{Version: Version, Keys: []PublicKey{pub}}
	env, err := Sign(priv, "tx-1", "digest-1", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, env, "tx-1", "digest-1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	bad := env
	bad.TransactionDigest = "changed"
	if Verify(ring, bad, "tx-1", "changed", now.Add(time.Minute)) == nil {
		t.Fatal("tampered envelope accepted")
	}
	if Verify(ring, env, "tx-1", "digest-1", now.Add(2*time.Hour)) == nil {
		t.Fatal("expired envelope accepted")
	}
}

func TestPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	priv, pub, _ := Generate("operator", now)
	privatePath := filepath.Join(dir, "private.json")
	ringPath := filepath.Join(dir, "ring.json")
	if err := WritePrivate(privatePath, priv); err != nil {
		t.Fatal(err)
	}
	if err := WriteKeyring(ringPath, Keyring{Version: Version, Keys: []PublicKey{pub}}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivate(privatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyring(ringPath); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(priv)
	if string(b) == "" {
		t.Fatal("marshal")
	}
	if err := os.Chmod(privatePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivate(privatePath); err == nil {
		t.Fatal("weak permissions accepted")
	}
}

func TestRotateAndRevokeKeyring(t *testing.T) {
	now := time.Now().UTC()
	oldPriv, oldPub, err := Generate("alice@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	ring := Keyring{Version: Version, Keys: []PublicKey{oldPub}}
	ring, newPriv, newPub, err := Rotate(ring, "alice@example.com", false, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(ring.Keys) != 2 || !ring.Keys[0].Enabled || !ring.Keys[1].Enabled {
		t.Fatalf("ring=%+v", ring)
	}
	oldEnv, _ := Sign(oldPriv, "tx", "digest", time.Hour, now)
	newEnv, _ := Sign(newPriv, "tx", "digest", time.Hour, now)
	if err := Verify(ring, oldEnv, "tx", "digest", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ring, newEnv, "tx", "digest", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ring, err = SetEnabled(ring, oldPub.KeyID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if Verify(ring, oldEnv, "tx", "digest", now.Add(2*time.Minute)) == nil {
		t.Fatal("disabled key accepted")
	}
	if err := Verify(ring, newEnv, "tx", "digest", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := SetEnabled(ring, newPub.KeyID, false, false); err == nil {
		t.Fatal("final enabled key disabled")
	}
}
