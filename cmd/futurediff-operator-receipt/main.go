package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/operatorreceipt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	home, _ := os.UserHomeDir()
	root := flag.String("root", filepath.Join(home, ".futurediff"), "data root")
	mode := flag.String("mode", "verify", "record or verify")
	private := flag.String("private", "", "operator private key")
	keyring := flag.String("keyring", "", "operator public keyring")
	action := flag.String("action", "", "action")
	actor := flag.String("actor", "", "actor")
	subject := flag.String("subject", "", "subject")
	reason := flag.String("reason", "", "reason")
	version := flag.Bool("version", false, "version")
	flag.Parse()
	if *version {
		emit(buildinfo.Current())
		return
	}
	dir := filepath.Join(*root, "operator-receipts")
	switch *mode {
	case "record":
		if *private == "" {
			fail(fmt.Errorf("private key required"))
		}
		k, e := operatorapproval.LoadPrivate(*private)
		if e != nil {
			fail(e)
		}
		r, e := operatorreceipt.Record(dir, k, *action, *actor, *subject, *reason, time.Now())
		if e != nil {
			fail(e)
		}
		emit(r)
	case "verify":
		if *keyring == "" {
			fail(fmt.Errorf("keyring required"))
		}
		r, e := operatorapproval.LoadKeyring(*keyring)
		if e != nil {
			fail(e)
		}
		v, e := operatorreceipt.Verify(dir, r, time.Now())
		if e != nil {
			fail(e)
		}
		emit(v)
		if !v.Valid {
			os.Exit(2)
		}
	default:
		fail(fmt.Errorf("unsupported mode %s", *mode))
	}
}
func emit(v any)   { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
