package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/idempotencygc"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	policyPath := flag.String("policy", "", "idempotency retention policy JSON")
	apply := flag.Bool("apply", false, "apply plan")
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
	p, e := idempotencygc.Load(*policyPath)
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
	plan, e := idempotencygc.BuildPlan(repo, p, time.Now())
	if e != nil {
		fail(e)
	}
	out := map[string]any{"plan": plan, "confirmation_required": idempotencygc.Confirmation}
	if *apply {
		res, e := idempotencygc.Apply(repo, plan, *confirm, time.Now())
		out["result"] = res
		if e != nil {
			emit(out)
			fail(e)
		}
	}
	emit(out)
	if !plan.WithinLimits {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
