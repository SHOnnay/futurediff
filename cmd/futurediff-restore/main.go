package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/ledgerrestore"
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
	// Human reconciliation guidance goes to stderr so stdout stays a pure
	// JSON stream for scripts. The guidance never prompts and never runs the
	// recovery command; it only tells the operator what to run.
	if g := humanGuidance(r); g != "" {
		fmt.Fprintln(os.Stderr, g)
	}
}

// humanGuidance renders the post-restore external-effect comparison for a
// human reader: one of the four stable summary states plus the exact
// canonical recovery/reconciliation commands to run (never executed here).
func humanGuidance(r ledgerrestore.Report) string {
	rec := r.EffectReconciliation
	if rec == nil {
		return ""
	}
	lines := []string{rec.HumanSummary}
	for _, cmd := range rec.RecoveryCommands {
		lines = append(lines, "run: "+cmd)
	}
	if rec.RecommendedAction != "" && rec.RecommendedAction != rec.HumanSummary {
		lines = append(lines, rec.RecommendedAction)
	}
	return strings.Join(lines, "\n")
}

func defaultRoot() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".futurediff") }
func fail(e error)        { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
