package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/integritycheckpoint"
	"os"
	"time"
)

func main() {
	mode := flag.String("mode", "create", "create or verify")
	root := flag.String("root", "", "FutureDiff data root")
	output := flag.String("output", "", "checkpoint JSON path")
	private := flag.String("private", "", "operator private key")
	keyring := flag.String("keyring", "", "trusted operator keyring")
	ledger := flag.String("ledger", "", "ledger backup override for verify")
	receipts := flag.String("receipt-dir", "", "optional operator receipt directory")
	flag.Parse()
	var out any
	var err error
	switch *mode {
	case "create":
		if *root == "" || *output == "" || *private == "" {
			fatal(fmt.Errorf("create requires --root --output --private"))
		}
		out, err = integritycheckpoint.Create(*root, *output, *private, *keyring, *receipts, time.Now())
	case "verify":
		if *output == "" || *keyring == "" {
			fatal(fmt.Errorf("verify requires --output --keyring"))
		}
		out, err = integritycheckpoint.Verify(*output, *keyring, *ledger, *receipts, time.Now())
	default:
		fatal(fmt.Errorf("unsupported mode %q", *mode))
	}
	if err != nil {
		if out != nil {
			encode(out)
		}
		fatal(err)
	}
	encode(out)
}
func encode(v any) {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
