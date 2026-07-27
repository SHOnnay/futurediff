package integritycheckpoint

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointRoundTrip(t *testing.T) {
	root := t.TempDir()
	r, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	priv := filepath.Join(root, "private.json")
	ring := filepath.Join(root, "ring.json")
	pk, pub, err := operatorapproval.Generate("op", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := operatorapproval.WritePrivate(priv, pk); err != nil {
		t.Fatal(err)
	}
	if err := operatorapproval.WriteKeyring(ring, operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "checkpoint.json")
	if _, err := Create(root, out, priv, ring, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	v, err := Verify(out, ring, "", "", time.Now())
	if err != nil || !v.Valid {
		t.Fatalf("%v %+v", err, v)
	}
}
