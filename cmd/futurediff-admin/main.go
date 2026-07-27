package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

func main() {
	rootDefault := ""
	if home, err := os.UserHomeDir(); err == nil {
		rootDefault = filepath.Join(home, ".futurediff")
	}
	root := flag.String("root", rootDefault, "FutureDiff data root")
	backup := flag.String("backup", "", "create a consistent ledger backup at this path")
	faultSelfTest := flag.String("fault-self-test", "", "run destructive ledger fault checks in a disposable directory")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if err := os.MkdirAll(*root, 0o700); err != nil {
		fail(err)
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fail(err)
	}
	defer repo.Close()
	result := map[string]any{}
	if *faultSelfTest != "" {
		report, err := ledger.RunFaultSelfTest(*faultSelfTest)
		if err != nil {
			fail(err)
		}
		result["fault_self_test"] = report
	}
	health, err := repo.HealthCheck()
	if err != nil {
		fail(err)
	}
	result["health"] = health
	if *backup != "" {
		record, err := repo.Backup(*backup)
		if err != nil {
			fail(err)
		}
		result["backup"] = record
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}
func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
