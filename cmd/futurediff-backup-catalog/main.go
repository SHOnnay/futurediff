package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/backupcatalog"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	policyPath := flag.String("policy", "", "backup policy JSON")
	apply := flag.Bool("apply", false, "delete planned backups")
	confirm := flag.String("confirm", "", "exact confirmation")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	if *policyPath == "" {
		fail(fmt.Errorf("policy required"))
	}
	p, e := backupcatalog.Load(*policyPath)
	if e != nil {
		fail(e)
	}
	var lock *daemonlock.Lock
	if *apply {
		lock, e = daemonlock.Acquire(filepath.Join(*root, "daemon.lock"), *root, time.Now())
		if e != nil {
			fail(fmt.Errorf("daemon must be offline: %w", e))
		}
		defer lock.Release()
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	report, e := backupcatalog.Evaluate(repo, p, time.Now())
	if e != nil {
		fail(e)
	}
	out := map[string]any{"report": report, "confirmation_required": backupcatalog.Confirmation}
	if *apply {
		res, e := backupcatalog.Apply(repo, report, *confirm, time.Now())
		out["result"] = res
		if e != nil {
			emit(out)
			fail(e)
		}
	}
	emit(out)
	if !report.Healthy || !report.WithinLimits {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
