package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"os"
	"strings"
	"time"
)

type listFlag []string

func (f *listFlag) String() string     { return strings.Join(*f, ",") }
func (f *listFlag) Set(v string) error { *f = append(*f, v); return nil }
func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "assemble":
		assemble(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	case "version":
		printJSON(buildinfo.Current())
	default:
		usage()
	}
}
func assemble(args []string) {
	fs := flag.NewFlagSet("assemble", flag.ExitOnError)
	var envs listFlag
	fs.Var(&envs, "envelope", "approval envelope JSON (repeatable)")
	out := fs.String("output", "", "bundle output")
	_ = fs.Parse(args)
	if len(envs) == 0 || *out == "" {
		usage()
	}
	items := make([]operatorapproval.Envelope, 0, len(envs))
	for _, p := range envs {
		e, err := operatorapproval.LoadEnvelope(p)
		must(err)
		items = append(items, e)
	}
	bundle, err := operatorapproval.NewBundle(items)
	must(err)
	must(operatorapproval.WriteBundle(*out, bundle))
	printJSON(bundle)
}
func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	bundlePath := fs.String("bundle", "", "bundle JSON")
	keyring := fs.String("keyring", "", "trusted keyring")
	policyPath := fs.String("policy", "", "quorum policy")
	tx := fs.String("transaction", "", "transaction id")
	digest := fs.String("digest", "", "transaction digest")
	_ = fs.Parse(args)
	if *bundlePath == "" || *keyring == "" || *policyPath == "" || *tx == "" || *digest == "" {
		usage()
	}
	b, err := operatorapproval.LoadBundle(*bundlePath)
	must(err)
	r, err := operatorapproval.LoadKeyring(*keyring)
	must(err)
	p, err := operatorapproval.LoadQuorumPolicy(*policyPath)
	must(err)
	result, err := operatorapproval.VerifyQuorum(r, p, b, *tx, *digest, time.Now())
	must(err)
	printJSON(result)
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: futurediff-approval-quorum <assemble|verify|version> [flags]")
	os.Exit(2)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
