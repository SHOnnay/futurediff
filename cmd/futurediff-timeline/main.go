package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/timeline"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	tx := flag.String("transaction", "", "transaction id")
	format := flag.String("format", "json", "json, markdown, or mermaid")
	out := flag.String("output", "", "optional output file")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		printJSON(buildinfo.Current())
		return
	}
	if *tx == "" {
		fail(fmt.Errorf("transaction is required"))
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fail(err)
	}
	defer repo.Close()
	r, err := timeline.Build(repo, *tx)
	if err != nil {
		fail(err)
	}
	var b []byte
	switch *format {
	case "json":
		b, _ = json.MarshalIndent(r, "", "  ")
		b = append(b, '\n')
	case "markdown":
		b = []byte(timeline.Markdown(r))
	case "mermaid":
		b = []byte(timeline.Mermaid(r))
	default:
		fail(fmt.Errorf("unsupported format %q", *format))
	}
	if *out != "" {
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			fail(err)
		}
	} else {
		fmt.Print(string(b))
	}
}
func fail(err error)  { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
