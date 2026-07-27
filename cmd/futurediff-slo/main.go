package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/slo"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	socket := flag.String("socket", "", "daemon socket")
	policyPath := flag.String("policy", "", "SLO policy")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	if *policyPath == "" {
		fail(fmt.Errorf("policy required"))
	}
	if *socket == "" {
		*socket = filepath.Join(*root, "futurediff.sock")
	}
	p, e := slo.Load(*policyPath)
	if e != nil {
		fail(e)
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	r, e := slo.Evaluate(repo, *socket, p, time.Now())
	if e != nil {
		fail(e)
	}
	emit(r)
	if r.Status != "pass" {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
