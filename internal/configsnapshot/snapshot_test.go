package configsnapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildVerifyAndDetectDrift(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Build([]Input{{Name: "daemon", Path: p, Required: true}}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if m.Entries[0].SHA256 == "" {
		t.Fatal("missing digest")
	}
	r, err := Verify(m, time.Unix(2, 0))
	if err != nil || !r.Verified {
		t.Fatalf("verify: %+v %v", r, err)
	}
	if err := os.WriteFile(p, []byte(`{"enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err = Verify(m, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.Verified {
		t.Fatal("drift should fail")
	}
}

func TestOptionalMissingAndSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if _, err := Build([]Input{{Name: "optional", Path: missing}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	_ = os.WriteFile(target, []byte("x"), 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Build([]Input{{Name: "bad", Path: link, Required: true}}, time.Now()); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestManifestTamperRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c")
	_ = os.WriteFile(p, []byte("a"), 0o600)
	m, _ := Build([]Input{{Name: "c", Path: p, Required: true}}, time.Now())
	m.Entries[0].SHA256 = "bad"
	if _, err := Verify(m, time.Now()); err == nil {
		t.Fatal("tampered manifest accepted")
	}
}
