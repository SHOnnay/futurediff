package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	m "github.com/SHOnnay/futurediff/internal/metrics"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", defaultRoot(), "FutureDiff data root")
	format := flag.String("format", "json", "json or prometheus")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	s, e := repo.Metrics()
	if e != nil {
		fail(e)
	}
	switch *format {
	case "json":
		b, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(b))
	case "prometheus":
		fmt.Print(m.Prometheus(s))
	default:
		fail(fmt.Errorf("unsupported format %s", *format))
	}
}
func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
