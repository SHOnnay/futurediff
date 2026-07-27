package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/replay"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", defaultRoot(), "FutureDiff data root")
	tx := flag.String("transaction", "", "transaction id")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *tx == "" {
		fail(fmt.Errorf("--transaction is required"))
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	r, e := replay.Transaction(repo, *tx)
	if e != nil {
		fail(e)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	if !r.Valid {
		os.Exit(2)
	}
}
func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
