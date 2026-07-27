package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SHOnnay/futurediff/internal/configattest"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

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
	file := fs.String("file", "", "configuration file")
	kind := fs.String("kind", "", "stable configuration kind")
	output := fs.String("output", "", "attestation output; defaults to FILE.fdattest.json")
	ttl := fs.Duration("ttl", 30*24*time.Hour, "attestation lifetime")
	_ = fs.Parse(args)
	if *private == "" || *file == "" || *kind == "" {
		usage()
	}
	if *output == "" {
		*output = configattest.SidecarPath(*file)
	}
	key, err := operatorapproval.LoadPrivate(*private)
	must(err)
	env, err := configattest.Sign(key, *file, *kind, *ttl, time.Now())
	must(err)
	must(configattest.Write(*output, env))
	printJSON(map[string]any{"output": *output, "kind": env.Kind, "file_sha256": env.FileSHA256, "approver": env.Approver, "key_id": env.KeyID, "expires_at": env.ExpiresAt})
}
func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	keyring := fs.String("keyring", "", "trusted operator keyring")
	file := fs.String("file", "", "configuration file")
	kind := fs.String("kind", "", "expected configuration kind")
	envelope := fs.String("envelope", "", "attestation; defaults to FILE.fdattest.json")
	_ = fs.Parse(args)
	if *keyring == "" || *file == "" || *kind == "" {
		usage()
	}
	if *envelope == "" {
		*envelope = configattest.SidecarPath(*file)
	}
	ring, err := operatorapproval.LoadKeyring(*keyring)
	must(err)
	env, err := configattest.Load(*envelope)
	must(err)
	must(configattest.Verify(ring, env, *file, *kind, time.Now()))
	printJSON(map[string]any{"verified": true, "kind": env.Kind, "file_sha256": env.FileSHA256, "approver": env.Approver, "key_id": env.KeyID, "expires_at": env.ExpiresAt})
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff-config-sign <sign|verify> [flags]")
	os.Exit(2)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	must(enc.Encode(v))
}
