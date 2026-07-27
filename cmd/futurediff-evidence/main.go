package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "generate-key":
		generate(os.Args[2:])
	case "init-keyring":
		initKeyring(os.Args[2:])
	case "rotate-keyring":
		rotateKeyring(os.Args[2:])
	case "encrypt":
		encrypt(os.Args[2:])
	case "decrypt":
		decrypt(os.Args[2:])
	case "decrypt-keyring":
		decryptKeyring(os.Args[2:])
	case "version":
		printJSON(buildinfo.Current())
	default:
		usage()
	}
}
func generate(args []string) {
	fs := flag.NewFlagSet("generate-key", flag.ExitOnError)
	out := fs.String("output", "", "0600 key output")
	_ = fs.Parse(args)
	if *out == "" {
		usage()
	}
	k, err := evidencecrypto.Generate(time.Now())
	must(err)
	must(evidencecrypto.WriteKey(*out, k))
	printJSON(map[string]any{"output": *out, "key_id": k.KeyID})
}
func encrypt(args []string) {
	fs := flag.NewFlagSet("encrypt", flag.ExitOnError)
	key := fs.String("key", "", "key file")
	in := fs.String("input", "", "plaintext input")
	out := fs.String("output", "", "encrypted output")
	aad := fs.String("aad", "", "associated data")
	_ = fs.Parse(args)
	if *key == "" || *in == "" || *out == "" {
		usage()
	}
	c, err := evidencecrypto.Load(*key)
	must(err)
	b, err := os.ReadFile(*in)
	must(err)
	must(c.WriteFile(*out, b, []byte(*aad)))
	printJSON(map[string]any{"output": *out, "key_id": c.KeyID, "encrypted": true})
}
func decrypt(args []string) {
	fs := flag.NewFlagSet("decrypt", flag.ExitOnError)
	key := fs.String("key", "", "key file")
	in := fs.String("input", "", "encrypted input")
	out := fs.String("output", "", "plaintext output")
	aad := fs.String("aad", "", "associated data")
	_ = fs.Parse(args)
	if *key == "" || *in == "" || *out == "" {
		usage()
	}
	c, err := evidencecrypto.Load(*key)
	must(err)
	b, err := c.ReadFile(*in, []byte(*aad))
	must(err)
	must(os.WriteFile(*out, b, 0o600))
	printJSON(map[string]any{"output": *out, "key_id": c.KeyID, "decrypted": true})
}

func initKeyring(args []string) {
	fs := flag.NewFlagSet("init-keyring", flag.ExitOnError)
	ring := fs.String("keyring", "", "keyring output")
	key := fs.String("key", "", "initial key output")
	_ = fs.Parse(args)
	if *ring == "" || *key == "" {
		usage()
	}
	f, k, err := evidencecrypto.InitializeKeyring(*ring, *key, time.Now())
	must(err)
	printJSON(map[string]any{"keyring": *ring, "active_key_id": f.ActiveKeyID, "key": *key, "key_id": k.KeyID})
}
func rotateKeyring(args []string) {
	fs := flag.NewFlagSet("rotate-keyring", flag.ExitOnError)
	ring := fs.String("keyring", "", "existing keyring")
	key := fs.String("new-key", "", "new key output")
	disableOld := fs.Bool("disable-old", false, "disable historical decrypt keys")
	_ = fs.Parse(args)
	if *ring == "" || *key == "" {
		usage()
	}
	f, k, err := evidencecrypto.RotateKeyring(*ring, *key, *disableOld, time.Now())
	must(err)
	printJSON(map[string]any{"keyring": *ring, "active_key_id": f.ActiveKeyID, "new_key": *key, "key_id": k.KeyID, "historical_keys_disabled": *disableOld})
}
func decryptKeyring(args []string) {
	fs := flag.NewFlagSet("decrypt-keyring", flag.ExitOnError)
	ringPath := fs.String("keyring", "", "evidence keyring")
	in := fs.String("input", "", "encrypted input")
	out := fs.String("output", "", "plaintext output")
	aad := fs.String("aad", "", "associated data")
	_ = fs.Parse(args)
	if *ringPath == "" || *in == "" || *out == "" {
		usage()
	}
	ring, err := evidencecrypto.LoadKeyring(*ringPath)
	must(err)
	b, err := ring.ReadFile(*in, []byte(*aad))
	must(err)
	must(os.WriteFile(*out, b, 0o600))
	printJSON(map[string]any{"output": *out, "active_key_id": ring.ActiveKeyID(), "decrypted": true})
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff-evidence <generate-key|init-keyring|rotate-keyring|encrypt|decrypt|decrypt-keyring|version> [flags]")
	os.Exit(2)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
