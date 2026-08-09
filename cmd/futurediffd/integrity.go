package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// startupIntegrityError is the fail-closed refusal returned by the
// --require-integrity startup gate. Error() renders a stable reason code and
// an actionable recommendation with no paths, credentials, or secrets.
type startupIntegrityError struct {
	ReasonCode        string
	RecommendedAction string
	message           string
}

func (e *startupIntegrityError) Error() string {
	return fmt.Sprintf("ledger integrity gate refused startup: reason_code=%s; %s; recommended action: %s", e.ReasonCode, e.message, e.RecommendedAction)
}

// daemonDiagnose is the offline diagnosis entry point used by the startup
// integrity gate. It is a package-level seam so startup tests can exercise
// the gate against diagnosis states SQLite cannot produce from a real file
// set (wal_inconsistent is deferred upstream). Production uses
// ledger.Diagnose.
var daemonDiagnose = ledger.Diagnose

// openRepository is a seam over ledger.OpenRepository so tests can prove the
// integrity gate refuses startup before any read-write open or migration.
var openRepository = ledger.OpenRepository

// openLedgerForStartup performs the ordered startup sequence: exclusive
// daemon ownership (flock), the optional fail-closed integrity gate, and the
// read-write ledger open. The gate runs only when requireIntegrity is set and
// always before any ledger migration or socket creation. On any failure the
// lock acquired by this attempt is released and repo is nil; pre-existing
// foreign or ambiguous runtime artifacts are never touched. When
// requireIntegrity is absent, behavior matches the historical startup path.
func openLedgerForStartup(root, lockFile string, requireIntegrity bool) (*daemonlock.Lock, *ledger.Repository, error) {
	lock, err := daemonlock.Acquire(lockFile, root, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if requireIntegrity {
		if err := checkStartupIntegrity(filepath.Join(root, "ledger.db")); err != nil {
			_ = lock.Release()
			return nil, nil, err
		}
	}
	repo, err := openRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		_ = lock.Release()
		return nil, nil, err
	}
	return lock, repo, nil
}

// checkStartupIntegrity runs the non-mutating offline diagnosis with
// Quiescent=true and FullIntegrity=true against the authoritative ledger. A
// missing ledger is a valid first start (the read-write open initializes it
// afterwards); every other non-healthy state refuses startup. It never
// repairs, restores, checkpoints, or deletes anything, and never touches
// another daemon's runtime files.
func checkStartupIntegrity(ledgerPath string) error {
	d, err := daemonDiagnose(ledgerPath, ledger.DiagnoseOptions{Quiescent: true, FullIntegrity: true})
	if err != nil {
		return &startupIntegrityError{
			ReasonCode:        string(ledger.StorageIOFailure),
			RecommendedAction: "check disk space, quotas, and filesystem permissions, then retry startup",
			message:           fmt.Sprintf("offline ledger diagnosis failed: %v", err),
		}
	}
	switch d.State {
	case ledger.Healthy:
		return nil
	case ledger.DatabaseNotInitialized:
		// Valid first-start state: initialization happens after the gate.
		return nil
	}
	code := d.ReasonCode
	if code == "" {
		code = string(d.State)
	}
	message := d.Message
	if message == "" {
		message = "ledger state " + string(d.State) + " is not safe for startup"
	}
	return &startupIntegrityError{ReasonCode: code, RecommendedAction: startupRefusalAction(d.State), message: message}
}

// startupRefusalAction maps a refused diagnosis state to an actionable
// recommendation. All refusal paths are non-destructive: the daemon never
// repairs, restores, checkpoints, or removes files on its own.
func startupRefusalAction(state ledger.DiagnosisState) string {
	switch state {
	case ledger.LedgerCorrupt, ledger.LedgerIntegrityFailed:
		return "restore the ledger from a verified backup; preserve the corrupted files as evidence"
	case ledger.WALInconsistent:
		return "resolve the WAL/SHM state or restore from a verified backup; preserve the files as evidence"
	case ledger.StorageIOFailure:
		return "check disk space, quotas, and filesystem permissions, then retry startup"
	default: // diagnosis_inconclusive and any unknown state
		return "stop any other process using the ledger and retry startup"
	}
}
