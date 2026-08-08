package guidedcli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// doctorCheck is one row of the fdif doctor checks table. The JSON keys are
// the Go field names (ID/Status/Detail), matching the historical output.
type doctorCheck struct {
	ID     string
	Status string
	Detail string
}

// ledgerDiagnosisResult is the structured ledger outcome surfaced by fdif
// doctor. It carries only stable, safe fields: no absolute paths, no
// environment dumps, no credentials, tokens, or headers.
type ledgerDiagnosisResult struct {
	ReasonCode         string   `json:"reason_code,omitempty"`
	State              string   `json:"state,omitempty"`
	IntegrityStatus    string   `json:"integrity_status,omitempty"`
	DiagnosisPerformed bool     `json:"diagnosis_performed"`
	QuiescenceProved   bool     `json:"quiescence_proved"`
	DaemonStatus       string   `json:"daemon_status,omitempty"`
	RecommendedAction  string   `json:"recommended_action,omitempty"`
	Message            string   `json:"message,omitempty"`
	JournalMode        string   `json:"journal_mode,omitempty"`
	WALPresent         bool     `json:"wal_present,omitempty"`
	SHMPresent         bool     `json:"shm_present,omitempty"`
	QuickCheckOK       bool     `json:"quick_check_ok,omitempty"`
	IntegrityErrors    []string `json:"integrity_errors,omitempty"`
}

// testRevalidationHook is a test-only hook invoked immediately before the
// final quiescence revalidation, after the initial probes have passed. Tests
// use it to make the socket reachable or change the lock at a deterministic
// point (never timing-based); production always leaves it nil.
var testRevalidationHook func(socketPath string)

// doctorDiagnose is the offline diagnosis entry point used by fdif doctor.
// It is a package-level seam so tests can exercise the full quiescence gate
// against a diagnosis state SQLite cannot produce from a real file set
// (wal_inconsistent is deferred upstream). Production uses ledger.Diagnose.
var doctorDiagnose = ledger.Diagnose

// diagnoseLedgerPath implements the offline ledger diagnosis for fdif doctor.
// The authoritative ledger is never opened through the repository API from
// here — that path runs migrations and can create or alter the ledger. The
// only ledger access is the non-mutating offline diagnosis API, invoked only
// after quiescence is proved and immediately revalidated. The doctor never
// creates, migrates, checkpoints, repairs, or modifies the ledger, and never
// deletes a lock or socket.
func (a *App) diagnoseLedgerPath(ctx context.Context, home string, daemonReachable bool) (ledgerDiagnosisResult, doctorCheck) {
	ledgerPath := filepath.Join(home, "ledger.db")
	lockPath := filepath.Join(home, "daemon.lock")
	socketPath := a.doctorSocketPath(home)

	// (A) A missing ledger is database_not_initialized: warn, non-fatal,
	// and never create the ledger or any sidecar. Nothing is diagnosed, so
	// no quiescence assertion is needed.
	if _, err := os.Lstat(ledgerPath); os.IsNotExist(err) {
		result := ledgerDiagnosisResult{
			ReasonCode:        "database_not_initialized",
			State:             "database_not_initialized",
			IntegrityStatus:   "warn",
			RecommendedAction: "start the daemon to initialize the ledger (fdif daemon start)",
			Message:           "ledger database is not initialized",
		}
		return result, doctorCheck{ID: "ledger_integrity", Status: "warn", Detail: "ledger not initialized"}
	}

	// (C) An authenticated, reachable daemon owns the ledger: no raw
	// diagnosis. The daemon health API carries no ledger diagnosis data, so
	// the outcome is inconclusive with a safe, actionable message.
	if daemonReachable {
		result := ledgerDiagnosisResult{
			ReasonCode:        "diagnosis_inconclusive",
			State:             "diagnosis_inconclusive",
			IntegrityStatus:   "warn",
			QuiescenceProved:  false,
			DaemonStatus:      "reachable",
			RecommendedAction: "stop the daemon (fdif daemon stop) and rerun fdif doctor for the offline ledger diagnosis",
			Message:           "ledger is owned by a running daemon; offline diagnosis was not performed",
		}
		return result, doctorCheck{ID: "ledger_integrity", Status: "warn", Detail: "ledger diagnosis deferred while the daemon is running"}
	}

	// Quiescence gate (B, D, E): the daemon did not answer the health probe.
	// Ownership is established from the daemon lock and the socket; every
	// ambiguous or liveness signal fails closed.
	status, inspectErr := daemonlock.Inspect(lockPath, time.Now())
	outcome := assessLedgerQuiescence(quiescenceProbe{
		lockStatus:    status,
		lockErr:       inspectErr,
		socketReached: dialUnixSocket(socketPath),
	})
	if !outcome.quiescent {
		return inconclusiveLedgerOutcome(outcome)
	}

	// (E) Revalidate immediately before diagnosis so a daemon that started
	// between the first probe and now cannot be missed.
	if testRevalidationHook != nil {
		testRevalidationHook(socketPath)
	}
	recheck, recheckErr := daemonlock.Inspect(lockPath, time.Now())
	outcome = assessLedgerQuiescence(quiescenceProbe{
		lockStatus:    recheck,
		lockErr:       recheckErr,
		socketReached: dialUnixSocket(socketPath),
	})
	if !outcome.quiescent {
		return inconclusiveLedgerOutcome(outcome)
	}

	// (B) Daemon conclusively stopped and no live owner: diagnose offline.
	d, _ := doctorDiagnose(ledgerPath, ledger.DiagnoseOptions{Quiescent: true})
	result := diagnosisToLedgerResult(d)
	return result, doctorCheck{ID: "ledger_integrity", Status: result.IntegrityStatus, Detail: result.Message}
}

// doctorSocketPath returns the daemon socket the doctor must probe: the
// configured socket when set, otherwise the default derived from home.
func (a *App) doctorSocketPath(home string) string {
	if strings.TrimSpace(a.Socket) != "" {
		return a.Socket
	}
	return filepath.Join(home, "futurediff.sock")
}

// quiescenceProbe is the evidence the doctor has about a possibly-stopped
// daemon: the lock inspection result and whether anything listens on the
// daemon socket.
type quiescenceProbe struct {
	lockStatus    daemonlock.Status
	lockErr       error
	socketReached bool
}

// quiescenceOutcome is the doctor's decision for one probe.
type quiescenceOutcome struct {
	quiescent         bool
	reasonCode        string
	daemonStatus      string
	message           string
	recommendedAction string
}

// assessLedgerQuiescence decides whether the ledger can be treated as
// quiescent. The daemon health probe has already failed; ownership is
// established here from the daemon lock and the socket. Every ambiguous or
// live signal fails closed — the ledger is only diagnosed when the daemon is
// conclusively stopped and no lock owner or socket listener is alive.
func assessLedgerQuiescence(p quiescenceProbe) quiescenceOutcome {
	st := p.lockStatus
	if p.lockErr != nil {
		code := st.ReasonCode
		if code == "" {
			code = "lock_owner_ambiguous"
		}
		return quiescenceOutcome{
			reasonCode:        code,
			daemonStatus:      "unavailable",
			message:           "daemon lock cannot be inspected; ledger ownership cannot be established",
			recommendedAction: "resolve the daemon lock state manually, then rerun fdif doctor",
		}
	}
	if st.OwnerStatus == "alive" {
		return quiescenceOutcome{
			reasonCode:        "diagnosis_inconclusive",
			daemonStatus:      "reachable",
			message:           "daemon lock is held by a live daemon owner; offline diagnosis was not performed",
			recommendedAction: "stop the daemon and rerun fdif doctor",
		}
	}
	if st.OwnerStatus == "ambiguous" {
		return quiescenceOutcome{
			reasonCode:        "lock_owner_ambiguous",
			daemonStatus:      "ambiguous",
			message:           "daemon lock owner is ambiguous; ledger ownership cannot be established",
			recommendedAction: "resolve the daemon/lock state first (inspect the daemon process), then rerun fdif doctor",
		}
	}
	if st.Held {
		// The flock is held by an unknown process whose recorded owner is
		// not provably alive; a live writer may still be active.
		return quiescenceOutcome{
			reasonCode:        "lock_owner_ambiguous",
			daemonStatus:      "ambiguous",
			message:           "daemon lock is held by an unknown process; the ledger may still be written",
			recommendedAction: "resolve the daemon/lock state first, then rerun fdif doctor",
		}
	}
	if p.socketReached {
		// Something listens on the daemon socket but the health probe was
		// unanswered: the daemon is not available for authenticated
		// confirmation, so quiescence cannot be proved.
		return quiescenceOutcome{
			reasonCode:        "daemon_unavailable",
			daemonStatus:      "unavailable",
			message:           "a process is listening on the daemon socket but the daemon did not answer the health probe; the ledger cannot be proved quiescent",
			recommendedAction: "stop the daemon and rerun fdif doctor",
		}
	}
	return quiescenceOutcome{quiescent: true, daemonStatus: "stopped"}
}

// inconclusiveLedgerOutcome renders a failed quiescence probe as the doctor
// ledger result. diagnosis_inconclusive is always warn — never healthy,
// never corrupt.
func inconclusiveLedgerOutcome(outcome quiescenceOutcome) (ledgerDiagnosisResult, doctorCheck) {
	result := ledgerDiagnosisResult{
		ReasonCode:        outcome.reasonCode,
		State:             "diagnosis_inconclusive",
		IntegrityStatus:   "warn",
		QuiescenceProved:  false,
		DaemonStatus:      outcome.daemonStatus,
		RecommendedAction: outcome.recommendedAction,
		Message:           outcome.message,
	}
	return result, doctorCheck{ID: "ledger_integrity", Status: "warn", Detail: outcome.message}
}

// diagnosisToLedgerResult maps an offline diagnosis to the stable doctor
// fields. Quiescence was proved and revalidated before the diagnosis ran, so
// quiescence_proved and diagnosis_performed are always true here.
func diagnosisToLedgerResult(d ledger.Diagnosis) ledgerDiagnosisResult {
	state := string(d.State)
	if state == "" {
		state = "diagnosis_inconclusive"
	}
	reason := d.ReasonCode
	if reason == "" {
		reason = state
	}
	status, action := ledgerIntegrityPresentation(d.State)
	return ledgerDiagnosisResult{
		ReasonCode:         reason,
		State:              state,
		IntegrityStatus:    status,
		DiagnosisPerformed: true,
		QuiescenceProved:   true,
		DaemonStatus:       "stopped",
		RecommendedAction:  action,
		Message:            d.Message,
		JournalMode:        d.JournalMode,
		WALPresent:         d.WALPresent,
		SHMPresent:         d.SHMPresent,
		QuickCheckOK:       d.QuickCheckOK,
		IntegrityErrors:    d.IntegrityErrors,
	}
}

// ledgerIntegrityPresentation maps a diagnosis state to the checks-table
// status and an actionable recommendation.
func ledgerIntegrityPresentation(state ledger.DiagnosisState) (status, action string) {
	switch state {
	case ledger.Healthy:
		return "pass", "none"
	case ledger.DatabaseNotInitialized:
		return "warn", "start the daemon to initialize the ledger (fdif daemon start)"
	case ledger.LedgerCorrupt, ledger.LedgerIntegrityFailed:
		return "fail", "restore the ledger from a verified backup; preserve the corrupted files as evidence"
	case ledger.WALInconsistent:
		return "fail", "resolve the WAL/SHM state or restore from a verified backup; preserve the files as evidence"
	case ledger.StorageIOFailure:
		return "fail", "check disk space, quotas, and filesystem permissions, then rerun fdif doctor"
	default: // ledger.DiagnosisInconclusive
		return "warn", "rerun fdif doctor after stopping the daemon, or use futurediff-doctor for the full report"
	}
}

// dialUnixSocket reports whether a process is listening on the daemon
// socket. A socket file with no listener (a stale socket) is not reachable.
func dialUnixSocket(path string) bool {
	if path == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
