package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "FutureDiff data root")
	limit := flag.Int("limit", 100, "recent decision limit")
	verify := flag.Bool("verify", false, "verify the complete authorization decision chain")
	flag.Parse()
	r, err := ledger.OpenRepository(filepath.Join(*root, "ledger.db"))
	if err != nil {
		fatal(err)
	}
	defer r.Close()
	if *verify {
		head, err := r.VerifyAuthorizationDecisionChain()
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(map[string]any{"valid": true, "head_digest": head}, "", "  ")
		fmt.Println(string(b))
		return
	}
	s, err := r.AuthorizationSummary(*limit)
	if err != nil {
		fatal(err)
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(b))
	if !s.ChainValid {
		os.Exit(1)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
