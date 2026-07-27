package operatorreceipt

import (
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordVerifyTamper(t *testing.T) {
	d := t.TempDir()
	priv, pub, err := operatorapproval.Generate("ops@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	if _, err = Record(d, priv, "maintenance.enable", "ops", "daemon", "upgrade", time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := Verify(d, ring, time.Now())
	if err != nil || !r.Valid || r.Count != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	files, _ := os.ReadDir(d)
	p := filepath.Join(d, files[0].Name())
	b, _ := os.ReadFile(p)
	b[len(b)/2] ^= 1
	_ = os.WriteFile(p, b, 0o600)
	v, verifyErr := Verify(d, ring, time.Now())
	if verifyErr == nil && v.Valid {
		t.Fatal("expected tamper detection")
	}
}
