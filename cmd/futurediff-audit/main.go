package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
)

func main() {
	rootDefault := ""
	if home, err := os.UserHomeDir(); err == nil {
		rootDefault = filepath.Join(home, ".futurediff")
	}
	root := flag.String("root", rootDefault, "FutureDiff data root")
	output := flag.String("output", "", "optional JSON report path")
	strict := flag.Bool("strict-warnings", false, "return failure when warnings exist")
	operatorEvents := flag.Bool("operator-events", false, "verify the local operator audit trail instead of the SQLite ledger audit")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	var (
		data []byte
		exit int
	)
	if *operatorEvents {
		auditRoot, err := filepath.Abs(*root)
		if err != nil {
			fail(err)
		}
		report, err := (&operatoraudit.Store{Root: auditRoot}).Verify()
		if err != nil {
			fail(err)
		}
		data, _ = json.MarshalIndent(report, "", "  ")
		if !report.Valid {
			exit = 2
		}
	} else {
		repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
		if err != nil {
			fail(err)
		}
		defer repo.Close()
		report, err := repo.Audit()
		if err != nil {
			fail(err)
		}
		data, _ = json.MarshalIndent(report, "", "  ")
		if !report.Healthy || (*strict && report.WarningCount > 0) {
			exit = 2
		}
	}
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o700); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*output, append(data, '\n'), 0o600); err != nil {
			fail(err)
		}
	}
	fmt.Println(string(data))
	if exit != 0 {
		os.Exit(exit)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
