package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/retention"
	"github.com/SHOnnay/futurediff/internal/retentionpolicy"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	policyPath := flag.String("policy", "", "policy JSON")
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
	p, e := retentionpolicy.Load(*policyPath)
	if e != nil {
		fail(e)
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	evaluation, e := retentionpolicy.Evaluate(repo, *root, p, time.Now())
	if e != nil {
		fail(e)
	}
	out := map[string]any{"evaluation": evaluation, "confirmation_required": retention.Confirmation}
	if *apply {
		res, e := retentionpolicy.Apply(repo, evaluation, *confirm)
		out["result"] = res
		if e != nil {
			emit(out)
			fail(e)
		}
	}
	emit(out)
	if !evaluation.WithinLimits {
		os.Exit(2)
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
