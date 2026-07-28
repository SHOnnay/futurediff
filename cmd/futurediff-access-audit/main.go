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
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	limit := flag.Int("limit", 100, "recent event limit")
	verify := flag.Bool("verify", false, "verify complete transaction-access chain")
	flag.Parse()
	r, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fatal(err)
	}
	defer r.Close()
	head, verr := r.VerifyTransactionAccessChain()
	if *verify {
		if verr != nil {
			fatal(verr)
		}
		print(map[string]any{"valid": true, "head_digest": head})
		return
	}
	events, err := r.TransactionAccessEvents(*limit)
	if err != nil {
		fatal(err)
	}
	print(map[string]any{"chain_valid": verr == nil, "chain_error": errorText(verr), "head_digest": head, "events": events})
	if verr != nil {
		os.Exit(1)
	}
}
func print(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func errorText(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
func fatal(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
