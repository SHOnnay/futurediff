package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/futurepack"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/transactionsnapshot"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", defaultRoot(), "FutureDiff data root")
	tx := flag.String("transaction", "", "transaction id")
	out := flag.String("output", "", "output .futurepack path")
	verify := flag.String("verify", "", "verify an existing .futurepack")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *verify != "" {
		m, e := futurepack.VerifyArchive(*verify)
		if e != nil {
			fail(e)
		}
		printJSON(map[string]any{"verified": true, "manifest": m})
		return
	}
	if *tx == "" || *out == "" {
		fail(fmt.Errorf("--transaction and --output are required"))
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	r, e := transactionsnapshot.Export(repo, *tx, *out)
	if e != nil {
		fail(e)
	}
	printJSON(r)
}
func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func printJSON(v any)     { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
