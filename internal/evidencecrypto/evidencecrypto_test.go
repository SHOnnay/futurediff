package evidencecrypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTripTamperAndAAD(t *testing.T) {
	k, err := Generate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "key.json")
	if err := WriteKey(p, k); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("tx:exec:stdout")
	enc, err := c.Seal([]byte("secret evidence"), aad)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Open(enc, aad)
	if err != nil || !bytes.Equal(got, []byte("secret evidence")) {
		t.Fatalf("roundtrip %v %q", err, got)
	}
	if _, err := c.Open(enc, []byte("wrong")); err == nil {
		t.Fatal("wrong AAD accepted")
	}
	enc[len(enc)-1] ^= 1
	if _, err := c.Open(enc, aad); err == nil {
		t.Fatal("tamper accepted")
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("weak permissions accepted")
	}
}
