package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/readiness"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	socket := flag.String("socket", "", "daemon socket")
	manifest := flag.String("manifest", "", "readiness manifest")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	if *manifest == "" {
		fail(fmt.Errorf("manifest required"))
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	m, e := readiness.Load(*manifest)
	if e != nil {
		fail(e)
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	r, e := readiness.Evaluate(repo, *root, *socket, m, time.Now())
	if e != nil {
		fail(e)
	}
	emit(r)
	if !r.Ready {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
