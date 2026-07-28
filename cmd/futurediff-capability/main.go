package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/capability"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"os"
	"strconv"
	"time"
)

type signedOutput struct {
	Token   capability.Token `json:"token"`
	Compact string           `json:"compact"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		usage()
	}
}
func sign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	private := fs.String("private", "", "operator private key")
	uidText := fs.String("uid", "", "subject Unix UID")
	op := fs.String("operation", "", "operation id")
	resource := fs.String("resource", "", "transaction/resource id")
	ttl := fs.Duration("ttl", 5*time.Minute, "capability lifetime, maximum 15m")
	output := fs.String("output", "", "output JSON path")
	_ = fs.Parse(args)
	if *private == "" || *uidText == "" || *op == "" {
		fatal(fmt.Errorf("--private, --uid and --operation are required"))
	}
	uid, err := strconv.ParseUint(*uidText, 10, 32)
	if err != nil {
		fatal(err)
	}
	key, err := operatorapproval.LoadPrivate(*private)
	if err != nil {
		fatal(err)
	}
	tok, err := capability.Sign(key, uint32(uid), *op, *resource, *ttl, time.Now())
	if err != nil {
		fatal(err)
	}
	compact, err := capability.EncodeCompact(tok)
	if err != nil {
		fatal(err)
	}
	b, _ := json.MarshalIndent(signedOutput{Token: tok, Compact: compact}, "", "  ")
	b = append(b, '\n')
	if *output != "" {
		if err := os.WriteFile(*output, b, 0o600); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Print(string(b))
}
func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	keyring := fs.String("keyring", "", "trusted keyring")
	input := fs.String("input", "", "signed output JSON or compact token file")
	uidText := fs.String("uid", "", "subject Unix UID")
	op := fs.String("operation", "", "operation id")
	resource := fs.String("resource", "", "transaction/resource id")
	_ = fs.Parse(args)
	if *keyring == "" || *input == "" || *uidText == "" || *op == "" {
		fatal(fmt.Errorf("--keyring, --input, --uid and --operation are required"))
	}
	uid, err := strconv.ParseUint(*uidText, 10, 32)
	if err != nil {
		fatal(err)
	}
	ring, err := operatorapproval.LoadKeyring(*keyring)
	if err != nil {
		fatal(err)
	}
	b, err := os.ReadFile(*input)
	if err != nil {
		fatal(err)
	}
	var out signedOutput
	tok := capability.Token{}
	if json.Unmarshal(b, &out) == nil && out.Token.CapabilityID != "" {
		tok = out.Token
	} else {
		tok, err = capability.DecodeCompact(string(b))
		if err != nil {
			fatal(err)
		}
	}
	if err := capability.Verify(ring, tok, uint32(uid), *op, *resource, time.Now()); err != nil {
		fatal(err)
	}
	result, _ := json.MarshalIndent(map[string]any{"valid": true, "capability_id": tok.CapabilityID, "digest": capability.Digest(tok), "expires_at": tok.ExpiresAt}, "", "  ")
	fmt.Println(string(result))
}
func usage()          { fmt.Fprintln(os.Stderr, "usage: futurediff-capability <sign|verify> ..."); os.Exit(2) }
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
