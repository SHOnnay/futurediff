package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/retention"
	"os"
	"path/filepath"
	"time"
)

func main() {
	rootDefault := ""
	if h, e := os.UserHomeDir(); e == nil {
		rootDefault = filepath.Join(h, ".futurediff")
	}
	root := flag.String("root", rootDefault, "FutureDiff data root")
	older := flag.Duration("older-than", 30*24*time.Hour, "only terminal transactions older than this duration")
	apply := flag.Bool("apply", false, "apply the generated plan")
	confirm := flag.String("confirm", "", "required exact confirmation when applying")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *older < 0 {
		fail(fmt.Errorf("older-than must be non-negative"))
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	plan, e := retention.BuildPlan(repo, *root, time.Now().UTC().Add(-*older))
	if e != nil {
		fail(e)
	}
	out := map[string]any{"plan": plan, "confirmation_required": retention.Confirmation}
	if *apply {
		res, e := retention.Apply(repo, plan, *confirm)
		out["result"] = res
		if e != nil {
			emit(out)
			fail(e)
		}
	}
	emit(out)
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
