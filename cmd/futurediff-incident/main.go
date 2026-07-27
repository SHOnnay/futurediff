package main

import (
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/incident"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	tx := flag.String("transaction", "", "transaction id")
	format := flag.String("format", "markdown", "markdown or json")
	out := flag.String("output", "", "optional output")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *tx == "" {
		fail(fmt.Errorf("--transaction is required"))
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fail(err)
	}
	defer repo.Close()
	r, err := incident.Build(repo, *tx, time.Now())
	if err != nil {
		fail(err)
	}
	var data []byte
	if *format == "json" {
		data, err = incident.JSON(r)
	} else if *format == "markdown" {
		data = []byte(incident.Markdown(r))
	} else {
		fail(fmt.Errorf("unsupported format %q", *format))
	}
	if err != nil {
		fail(err)
	}
	if *out != "" {
		if err := os.WriteFile(*out, data, 0o600); err != nil {
			fail(err)
		}
	} else {
		fmt.Print(string(data))
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
