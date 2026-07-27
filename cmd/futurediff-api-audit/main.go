package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SHOnnay/futurediff/internal/ledger"
)

func main() {
	root := flag.String("root", "", "FutureDiff data root")
	limit := flag.Int("limit", 100, "maximum recent events (1-1000)")
	verifyOnly := flag.Bool("verify", false, "verify the complete tamper-evident API access chain")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "--root is required")
		os.Exit(2)
	}
	repo, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fatal(err)
	}
	defer repo.Close()
	if *verifyOnly {
		head, err := repo.VerifyAPIAccessChain()
		if err != nil {
			fatal(err)
		}
		printJSON(map[string]any{"verified": true, "head_digest": head})
		return
	}
	summary, err := repo.APIAccessSummary(*limit)
	if err != nil {
		fatal(err)
	}
	printJSON(summary)
	if !summary.ChainValid {
		os.Exit(1)
	}
}
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
