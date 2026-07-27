package compatibility

import (
	"encoding/json"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"os"
	"path/filepath"
	"testing"
)

func TestRunAPIContract(t *testing.T) {
	d := t.TempDir()
	b, _ := json.MarshalIndent(apicontract.Current(), "", "  ")
	os.WriteFile(filepath.Join(d, "api.json"), b, 0o600)
	m := Manifest{FormatVersion: Version, APIContracts: []string{"api.json"}}
	mb, _ := json.Marshal(m)
	path := filepath.Join(d, "manifest.json")
	os.WriteFile(path, mb, 0o600)
	r, e := Run(path)
	if e != nil {
		t.Fatal(e)
	}
	if !r.Compatible || r.Passed != 1 {
		t.Fatal("compatibility")
	}
}
func TestTraversal(t *testing.T) {
	d := t.TempDir()
	m := Manifest{FormatVersion: Version, APIContracts: []string{"../x.json"}}
	mb, _ := json.Marshal(m)
	path := filepath.Join(d, "manifest.json")
	os.WriteFile(path, mb, 0o600)
	if _, e := Run(path); e == nil {
		t.Fatal("traversal accepted")
	}
}
