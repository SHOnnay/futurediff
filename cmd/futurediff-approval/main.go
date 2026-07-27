package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "generate":
		generate(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	case "rotate":
		rotate(os.Args[2:])
	case "set-enabled":
		setEnabled(os.Args[2:])
	case "list":
		list(os.Args[2:])
	case "version":
		printJSON(buildinfo.Current())
	default:
		usage()
	}
}
func generate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	approver := fs.String("approver", "", "operator identity")
	private := fs.String("private", "", "private key output")
	keyring := fs.String("keyring", "", "trusted keyring output")
	_ = fs.Parse(args)
	if *approver == "" || *private == "" || *keyring == "" {
		usage()
	}
	priv, pub, err := operatorapproval.Generate(*approver, time.Now())
	must(err)
	must(operatorapproval.WritePrivate(*private, priv))
	must(operatorapproval.WriteKeyring(*keyring, operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}))
	printJSON(map[string]any{"key_id": pub.KeyID, "approver": pub.Approver, "private_file": *private, "keyring": *keyring})
}
func sign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	private := fs.String("private", "", "private key file")
	tx := fs.String("transaction", "", "transaction id")
	digest := fs.String("digest", "", "approval digest")
	out := fs.String("output", "", "envelope output")
	ttl := fs.Duration("ttl", 15*time.Minute, "approval lifetime")
	_ = fs.Parse(args)
	if *private == "" || *tx == "" || *digest == "" || *out == "" {
		usage()
	}
	key, err := operatorapproval.LoadPrivate(*private)
	must(err)
	env, err := operatorapproval.Sign(key, *tx, *digest, *ttl, time.Now())
	must(err)
	b, _ := json.MarshalIndent(env, "", "  ")
	must(os.WriteFile(*out, append(b, '\n'), 0o600))
	printJSON(map[string]any{"output": *out, "key_id": env.KeyID, "expires_at": env.ExpiresAt})
}
func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	keyring := fs.String("keyring", "", "trusted keyring")
	envelope := fs.String("envelope", "", "approval envelope")
	tx := fs.String("transaction", "", "expected transaction")
	digest := fs.String("digest", "", "expected digest")
	_ = fs.Parse(args)
	if *keyring == "" || *envelope == "" || *tx == "" || *digest == "" {
		usage()
	}
	ring, err := operatorapproval.LoadKeyring(*keyring)
	must(err)
	b, err := os.ReadFile(*envelope)
	must(err)
	var env operatorapproval.Envelope
	must(json.Unmarshal(b, &env))
	must(operatorapproval.Verify(ring, env, *tx, *digest, time.Now()))
	printJSON(map[string]any{"verified": true, "approver": env.Approver, "key_id": env.KeyID, "signature_ref": operatorapproval.SignatureReference(env), "expires_at": env.ExpiresAt})
}

func rotate(args []string) {
	fs := flag.NewFlagSet("rotate", flag.ExitOnError)
	keyringPath := fs.String("keyring", "", "trusted keyring to update")
	approver := fs.String("approver", "", "operator identity")
	private := fs.String("private", "", "new private key output")
	disableOld := fs.Bool("disable-old", false, "disable older keys for the same approver")
	_ = fs.Parse(args)
	if *keyringPath == "" || *approver == "" || *private == "" {
		usage()
	}
	ring, err := operatorapproval.LoadKeyring(*keyringPath)
	must(err)
	ring, priv, pub, err := operatorapproval.Rotate(ring, *approver, *disableOld, time.Now())
	must(err)
	must(operatorapproval.WritePrivate(*private, priv))
	must(operatorapproval.WriteKeyring(*keyringPath, ring))
	printJSON(map[string]any{"keyring": *keyringPath, "private_file": *private, "new_key_id": pub.KeyID, "disable_old": *disableOld})
}
func setEnabled(args []string) {
	fs := flag.NewFlagSet("set-enabled", flag.ExitOnError)
	keyringPath := fs.String("keyring", "", "trusted keyring")
	keyID := fs.String("key-id", "", "key id")
	enabled := fs.Bool("enabled", true, "new enabled state")
	allowNone := fs.Bool("allow-no-enabled", false, "allow disabling the final key for an approver")
	_ = fs.Parse(args)
	if *keyringPath == "" || *keyID == "" {
		usage()
	}
	ring, err := operatorapproval.LoadKeyring(*keyringPath)
	must(err)
	ring, err = operatorapproval.SetEnabled(ring, *keyID, *enabled, *allowNone)
	must(err)
	must(operatorapproval.WriteKeyring(*keyringPath, ring))
	printJSON(map[string]any{"keyring": *keyringPath, "key_id": *keyID, "enabled": *enabled})
}
func list(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	keyringPath := fs.String("keyring", "", "trusted keyring")
	_ = fs.Parse(args)
	if *keyringPath == "" {
		usage()
	}
	ring, err := operatorapproval.LoadKeyring(*keyringPath)
	must(err)
	printJSON(ring)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff-approval <generate|sign|verify|rotate|set-enabled|list|version> [flags]")
	os.Exit(2)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
