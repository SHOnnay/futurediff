package main

import (
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/diffsummary"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	tx := flag.String("transaction", "", "transaction id")
	format := flag.String("format", "markdown", "markdown or json")
	out := flag.String("output", "", "optional output file")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *tx == "" {
		fail(fmt.Errorf("--transaction is required"))
	}
	r, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer r.Close()
	snap, e := r.Snapshot(*tx)
	if e != nil {
		fail(e)
	}
	s, e := diffsummary.Build(snap)
	if e != nil {
		fail(e)
	}
	var data []byte
	if *format == "json" {
		data, e = diffsummary.JSON(s)
	} else if *format == "markdown" {
		data = []byte(diffsummary.Markdown(s))
	} else {
		fail(fmt.Errorf("unsupported format %q", *format))
	}
	if e != nil {
		fail(e)
	}
	if *out != "" {
		if e = os.WriteFile(*out, data, 0o600); e != nil {
			fail(e)
		}
	} else {
		fmt.Print(string(data))
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
