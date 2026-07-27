package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/effectgraph"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	tx := flag.String("transaction", "", "transaction id")
	format := flag.String("format", "json", "json, mermaid, or dot")
	out := flag.String("output", "", "output file")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	if *tx == "" {
		fail(fmt.Errorf("transaction required"))
	}
	repo, e := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if e != nil {
		fail(e)
	}
	defer repo.Close()
	g, e := effectgraph.Build(repo, *tx)
	if e != nil {
		fail(e)
	}
	var b []byte
	switch *format {
	case "json":
		b, _ = json.MarshalIndent(g, "", "  ")
		b = append(b, '\n')
	case "mermaid":
		b = []byte(effectgraph.Mermaid(g))
	case "dot":
		b = []byte(effectgraph.DOT(g))
	default:
		fail(fmt.Errorf("unsupported format"))
	}
	if *out != "" {
		if e := os.WriteFile(*out, b, 0o600); e != nil {
			fail(e)
		}
	} else {
		fmt.Print(string(b))
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
