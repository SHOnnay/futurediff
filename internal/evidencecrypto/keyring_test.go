package evidencecrypto

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyringRotationReadsOldAndWritesNew(t *testing.T) {
	dir := t.TempDir()
	ringPath := filepath.Join(dir, "ring.json")
	oldPath := filepath.Join(dir, "old.json")
	f, old, err := InitializeKeyring(ringPath, oldPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ring, err := LoadKeyring(ringPath)
	if err != nil {
		t.Fatal(err)
	}
	oldArtifact := filepath.Join(dir, "old.fde")
	if err := ring.WriteFile(oldArtifact, []byte("old"), []byte("aad")); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "new.json")
	f, newKey, err := RotateKeyring(ringPath, newPath, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if f.ActiveKeyID != newKey.KeyID || f.ActiveKeyID == old.KeyID {
		t.Fatal("rotation failed")
	}
	ring, err = LoadKeyring(ringPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ring.ReadFile(oldArtifact, []byte("aad"))
	if err != nil || string(b) != "old" {
		t.Fatalf("old read %q %v", b, err)
	}
	newArtifact := filepath.Join(dir, "new.fde")
	if err := ring.WriteFile(newArtifact, []byte("new"), []byte("aad2")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(newArtifact)
	id, _ := ArtifactKeyID(raw)
	if id != newKey.KeyID {
		t.Fatalf("new artifact key=%s", id)
	}
}
func TestDisableOldPreventsOldRead(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "ring")
	kp := filepath.Join(dir, "k1")
	_, _, _ = InitializeKeyring(rp, kp, time.Now())
	ring, _ := LoadKeyring(rp)
	artifact := filepath.Join(dir, "a")
	_ = ring.WriteFile(artifact, []byte("x"), nil)
	_, _, err := RotateKeyring(rp, filepath.Join(dir, "k2"), true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ring, _ = LoadKeyring(rp)
	if _, err := ring.ReadFile(artifact, nil); err == nil {
		t.Fatal("disabled old key read succeeded")
	}
}
