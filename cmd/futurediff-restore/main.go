package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledgerrestore"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", defaultRoot(), "FutureDiff data root")
	backup := flag.String("backup", "", "ledger backup path")
	expected := flag.String("expected-sha256", "", "expected backup SHA-256")
	pre := flag.String("pre-restore-backup", "", "optional pre-restore backup path")
	apply := flag.Bool("apply", false, "apply restore after validation")
	confirm := flag.String("confirm", "", "required confirmation phrase when applying")
	allowStale := flag.Bool("allow-stale-backup", false, "allow applying a backup older than the live ledger")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	r, e := ledgerrestore.Run(ledgerrestore.Options{LivePath: filepath.Join(*root, "ledger.db"), BackupPath: *backup, ExpectedSHA256: *expected, SocketPath: filepath.Join(*root, "futurediff.sock"), LockPath: filepath.Join(*root, "daemon.lock"), PreRestoreBackupPath: *pre, Apply: *apply, Confirmation: *confirm, AllowStaleBackup: *allowStale})
	if e != nil {
		fail(e)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
}
func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
