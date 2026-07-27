package supportbundle

import (
	"context"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndVerify(t *testing.T) {
	root := t.TempDir()
	r, e := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	out := filepath.Join(t.TempDir(), "bundle.zip")
	if _, e = Create(context.Background(), out, Options{DataRoot: root}); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(out); e != nil {
		t.Fatal(e)
	}
	if st, e := os.Stat(out); e != nil || st.Size() == 0 {
		t.Fatal("bundle missing")
	}
	found, e := ContainsForbidden(out, root)
	if e != nil {
		t.Fatal(e)
	}
	if found {
		t.Fatal("data root leaked")
	}
}
