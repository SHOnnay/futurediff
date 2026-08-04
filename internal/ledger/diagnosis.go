package ledger

/*
#cgo LDFLAGS: -lsqlite3
#include <stdlib.h>
#include <sqlite3.h>

static const char* fd_sqlite_errmsg(sqlite3 *db) { return sqlite3_errmsg(db); }
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// maxDiagnoseBytes bounds every file read performed by Diagnose (ADR-099
// bounded reads). Larger files are reported as diagnosis_inconclusive rather
// than being streamed.
const maxDiagnoseBytes = 256 << 20

const (
	diagnoseFileMode = 0o600
)

// DiagnosisState is the stable classification of a non-mutating ledger
// diagnosis. It is intentionally conservative: anything that cannot be
// established conclusively is never reported as healthy.
type DiagnosisState string

const (
	// DatabaseNotInitialized means ledger.db does not exist. This is not
	// corruption; the ledger has simply never been initialized.
	DatabaseNotInitialized DiagnosisState = "database_not_initialized"
	// Healthy means the diagnostic snapshot passed SQLite's quick_check.
	Healthy DiagnosisState = "healthy"
	// LedgerCorrupt means the database header is invalid or the file is
	// truncated: SQLite cannot interpret the file as a database.
	LedgerCorrupt DiagnosisState = "ledger_corrupt"
	// LedgerIntegrityFailed means the database opened but SQLite quick_check
	// reported errors or aborted.
	LedgerIntegrityFailed DiagnosisState = "ledger_integrity_failed"
	// WALInconsistent means the WAL/SHM sidecars are unusable: SQLite fails
	// against the full snapshot but the database alone is healthy.
	WALInconsistent DiagnosisState = "wal_inconsistent"
	// StorageIOFailure means a storage-capacity, permission, quota, or I/O
	// error prevented diagnosis. It is never classified as corruption.
	StorageIOFailure DiagnosisState = "storage_io_failure"
	// DiagnosisInconclusive means a coherent offline snapshot could not be
	// established or the evidence is ambiguous. Diagnosis fails closed.
	DiagnosisInconclusive DiagnosisState = "diagnosis_inconclusive"
)

// Diagnosis is the machine-readable result of Diagnose. JSON fields never
// contain absolute paths, environment dumps, secrets, or credentials.
type Diagnosis struct {
	State           DiagnosisState `json:"state"`
	ReasonCode      string         `json:"reason_code,omitempty"`
	Message         string         `json:"message,omitempty"`
	SQLiteVersion   string         `json:"sqlite_version,omitempty"`
	JournalMode     string         `json:"journal_mode,omitempty"`
	WALPresent      bool           `json:"wal_present,omitempty"`
	SHMPresent      bool           `json:"shm_present,omitempty"`
	QuickCheckOK    bool           `json:"quick_check_ok,omitempty"`
	IntegrityErrors []string       `json:"integrity_errors,omitempty"`
}

// Diagnose performs a bounded, non-mutating integrity diagnosis of a ledger
// database without going through the normal read-write repository path.
//
// The authoritative ledger.db, -wal, and -shm files are never created,
// deleted, renamed, truncated, checkpointed, or repaired. Diagnosis runs
// against a private snapshot copy in a temporary directory, which is removed
// before returning. The originals are stat-compared before and after; if they
// changed mid-diagnosis the result fails closed as diagnosis_inconclusive.
//
// A missing ledger.db is DatabaseNotInitialized, never corruption. A missing
// -wal or -shm sidecar is never corruption on its own: SHM is transient
// WAL-index state, not authoritative durable state. Storage-capacity,
// permission, quota, and I/O errors are StorageIOFailure, never corruption.
// Anything that cannot be established conclusively fails closed.
//
// The returned error is non-nil only for storage I/O failures (wrapped so
// errors.Is works) or snapshot-cleanup failures; all other outcomes are
// expressed through Diagnosis.State.
func Diagnose(path string) (Diagnosis, error) {
	sqliteVersion := SQLiteVersion()
	if path == "" {
		return inconclusive("database path required", sqliteVersion), nil
	}

	before, err := statAuthoritative(path)
	if err != nil {
		return storageFailure(fmt.Errorf("inspect ledger path: %w", err), sqliteVersion)
	}
	if !before.db.exists {
		return notInitialized(before, sqliteVersion), nil
	}
	if !before.db.regular {
		return Diagnosis{
			State:         StorageIOFailure,
			ReasonCode:    reasonCode(StorageIOFailure),
			Message:       "ledger database path is not a regular file",
			SQLiteVersion: sqliteVersion,
		}, nil
	}
	if before.db.size > maxDiagnoseBytes {
		return inconclusive("database exceeds bounded diagnostic size", sqliteVersion), nil
	}
	if before.wal.exists && before.wal.size > maxDiagnoseBytes {
		return inconclusive("WAL exceeds bounded diagnostic size", sqliteVersion), nil
	}
	if before.shm.exists && before.shm.size > maxDiagnoseBytes {
		return inconclusive("SHM exceeds bounded diagnostic size", sqliteVersion), nil
	}

	snap, err := createDiagnosticSnapshot(path, before)
	if err != nil {
		if isStorageError(err) {
			return storageFailure(fmt.Errorf("create diagnostic snapshot: %w", err), sqliteVersion)
		}
		return Diagnosis{
			State:         DiagnosisInconclusive,
			ReasonCode:    reasonCode(DiagnosisInconclusive),
			Message:       "cannot establish a coherent diagnostic snapshot",
			SQLiteVersion: sqliteVersion,
		}, err
	}

	d := diagnoseSnapshot(snap, sqliteVersion)

	if err := snap.cleanup(); err != nil {
		return d, fmt.Errorf("remove diagnostic snapshot: %w", err)
	}

	// Confirm the authoritative files were not altered while we worked. A
	// live-changing file set is not a coherent snapshot and fails closed.
	after, err := statAuthoritative(path)
	if err != nil {
		return inconclusive("authoritative ledger files changed during diagnosis", sqliteVersion), nil
	}
	if !before.sameAs(after) {
		return inconclusive("authoritative ledger files changed during diagnosis", sqliteVersion), nil
	}
	return d, nil
}

func reasonCode(s DiagnosisState) string {
	if s == Healthy {
		return ""
	}
	return string(s)
}

func defaultMessage(s DiagnosisState) string {
	switch s {
	case DatabaseNotInitialized:
		return "ledger database is not initialized"
	case Healthy:
		return "ledger database is healthy"
	case LedgerCorrupt:
		return "ledger database is corrupt or is not a valid SQLite database"
	case LedgerIntegrityFailed:
		return "ledger database failed SQLite quick_check"
	case WALInconsistent:
		return "WAL/SHM is inconsistent with the ledger database"
	case StorageIOFailure:
		return "storage I/O failure while inspecting the ledger"
	default:
		return "ledger diagnosis could not be completed conclusively"
	}
}

func inconclusive(message, sqliteVersion string) Diagnosis {
	return Diagnosis{
		State:         DiagnosisInconclusive,
		ReasonCode:    reasonCode(DiagnosisInconclusive),
		Message:       message,
		SQLiteVersion: sqliteVersion,
	}
}

func notInitialized(before authoritativeState, sqliteVersion string) Diagnosis {
	return Diagnosis{
		State:         DatabaseNotInitialized,
		ReasonCode:    reasonCode(DatabaseNotInitialized),
		Message:       "ledger database is not initialized",
		SQLiteVersion: sqliteVersion,
		WALPresent:    before.wal.exists,
		SHMPresent:    before.shm.exists,
	}
}

func storageFailure(err error, sqliteVersion string) (Diagnosis, error) {
	return Diagnosis{
		State:         StorageIOFailure,
		ReasonCode:    reasonCode(StorageIOFailure),
		Message:       "storage I/O failure while inspecting the ledger (" + storageErrorDescription(err) + ")",
		SQLiteVersion: sqliteVersion,
	}, err
}

func storageErrorDescription(err error) string {
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "no space left on device"
	case errors.Is(err, syscall.EDQUOT):
		return "disk quota exceeded"
	case errors.Is(err, syscall.EROFS):
		return "file system is read-only"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	case errors.Is(err, syscall.EIO):
		return "input/output error"
	default:
		return "storage I/O failure"
	}
}

func isStorageError(err error) bool {
	return errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, syscall.EROFS) ||
		errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EIO)
}

// authoritativeFile is the stat view of one authoritative ledger file.
type authoritativeFile struct {
	exists  bool
	regular bool
	size    int64
	modTime time.Time
}

type authoritativeState struct {
	db, wal, shm authoritativeFile
}

func statFile(p string) (authoritativeFile, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return authoritativeFile{}, nil
		}
		return authoritativeFile{}, err
	}
	return authoritativeFile{
		exists:  true,
		regular: fi.Mode().IsRegular(),
		size:    fi.Size(),
		modTime: fi.ModTime(),
	}, nil
}

func statAuthoritative(path string) (authoritativeState, error) {
	var st authoritativeState
	var err error
	if st.db, err = statFile(path); err != nil {
		return st, err
	}
	if st.wal, err = statFile(path + "-wal"); err != nil {
		return st, err
	}
	if st.shm, err = statFile(path + "-shm"); err != nil {
		return st, err
	}
	return st, nil
}

func (a authoritativeState) sameAs(b authoritativeState) bool {
	return a.db == b.db && a.wal == b.wal && a.shm == b.shm
}

// diagnosticSnapshot is a private, disposable copy of the authoritative
// ledger files. SQLite diagnosis runs exclusively against these copies.
type diagnosticSnapshot struct {
	dir       string
	dbPath    string
	walPath   string
	shmPath   string
	walExists bool
	shmExists bool
}

func createDiagnosticSnapshot(path string, before authoritativeState) (*diagnosticSnapshot, error) {
	dir, err := os.MkdirTemp("", "futurediff-diagnose-")
	if err != nil {
		return nil, err
	}
	snap := &diagnosticSnapshot{
		dir:     dir,
		dbPath:  filepath.Join(dir, "ledger.db"),
		walPath: filepath.Join(dir, "ledger.db-wal"),
		shmPath: filepath.Join(dir, "ledger.db-shm"),
	}
	fail := func(err error) (*diagnosticSnapshot, error) {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := copyBounded(path, snap.dbPath); err != nil {
		return fail(err)
	}
	if before.wal.exists {
		if err := copySidecar(path+"-wal", snap.walPath); err != nil {
			return fail(err)
		}
		snap.walExists = true
	}
	if before.shm.exists {
		if err := copySidecar(path+"-shm", snap.shmPath); err != nil {
			return fail(err)
		}
		snap.shmExists = true
	}
	return snap, nil
}

func copyBounded(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, diagnoseFileMode)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, maxDiagnoseBytes+1))
	if err != nil {
		return err
	}
	if n > maxDiagnoseBytes {
		return errors.New("file exceeds bounded diagnostic size")
	}
	return nil
}

// copySidecar replicates a transient sidecar into the snapshot. Regular files
// are copied byte-for-byte; directories are replicated as directories so that
// SQLite's own open/check behavior (SQLITE_CANTOPEN) becomes the evidence.
// Other non-regular kinds (symlinks, fifos, sockets) are left absent so the
// main database remains diagnosable.
func copySidecar(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode().IsRegular():
		return copyBounded(src, dst)
	case fi.IsDir():
		return os.Mkdir(dst, diagnoseFileMode)
	default:
		return nil
	}
}

func (s *diagnosticSnapshot) cleanup() error {
	if s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	s.dir = ""
	return err
}

// moveSidecarsAside renames the transient WAL/SHM copies within the private
// snapshot so the main database can be diagnosed alone. Only snapshot copies
// are touched; authoritative files are never renamed.
func (s *diagnosticSnapshot) moveSidecarsAside() error {
	for _, p := range []string{s.walPath, s.shmPath} {
		if _, err := os.Lstat(p); err == nil {
			if err := os.Rename(p, p+".aside"); err != nil {
				return err
			}
		}
	}
	return nil
}

// openDiagnostic opens an existing SQLite file without the CREATE flag and
// without executing any pragmas or migrations, so the connection cannot create
// or alter anything outside the private snapshot it is given. Read-write
// (rather than read-only) is deliberate: SQLite may rebuild the transient -shm
// index inside the private snapshot directory, which is exactly the evidence
// needed to tell a healthy WAL from an unusable one.
func openDiagnostic(path string) (*DB, C.int, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var raw *C.sqlite3
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_FULLMUTEX
	rc := C.sqlite3_open_v2(cpath, &raw, C.int(flags), nil)
	if rc != C.SQLITE_OK {
		msg := "sqlite open failed"
		if raw != nil {
			msg = C.GoString(C.fd_sqlite_errmsg(raw))
			C.sqlite3_close(raw)
		}
		return nil, rc, errors.New(msg)
	}
	db := &DB{db: raw}
	runtime.SetFinalizer(db, func(d *DB) { _ = d.Close() })
	return db, rc, nil
}

// stateForRC maps a SQLite result code from the journal-mode probe (or the
// initial open) to a diagnosis state. Invalid headers and truncation surface
// as NOTADB/CORRUPT here; unexpected codes fail closed as inconclusive.
func stateForRC(rc C.int) DiagnosisState {
	switch primaryRC(rc) {
	case int(C.SQLITE_NOTADB), int(C.SQLITE_CORRUPT):
		return LedgerCorrupt
	case int(C.SQLITE_CANTOPEN), int(C.SQLITE_IOERR), int(C.SQLITE_FULL), int(C.SQLITE_READONLY):
		return StorageIOFailure
	default:
		return DiagnosisInconclusive
	}
}

// stateForCheckRC maps a SQLite result code from quick_check itself to a
// diagnosis state. Corruption detected while the routine integrity gate runs
// is an integrity failure, not a header/truncation problem.
func stateForCheckRC(rc C.int) DiagnosisState {
	switch primaryRC(rc) {
	case int(C.SQLITE_NOTADB):
		return LedgerCorrupt
	case int(C.SQLITE_CANTOPEN), int(C.SQLITE_IOERR), int(C.SQLITE_FULL), int(C.SQLITE_READONLY):
		return StorageIOFailure
	default:
		return LedgerIntegrityFailed
	}
}

// primaryRC extracts the primary SQLite result code from a possibly extended
// code (extended codes encode the subclass in the upper bits).
func primaryRC(rc C.int) int {
	return int(rc) & 0xff
}

type connectionResult struct {
	state        DiagnosisState
	journalMode  string
	quickCheckOK bool
	errs         []string
	message      string
}

// diagnoseConnection opens one diagnostic connection against dbPath and runs
// the bounded routine checks: journal-mode probe, (optionally) truncation
// check, and PRAGMA quick_check. It never runs a WAL checkpoint.
func diagnoseConnection(dbPath string, checkTruncation bool) connectionResult {
	db, rc, err := openDiagnostic(dbPath)
	if err != nil {
		return connectionResult{state: stateForRC(rc), message: "cannot open diagnostic snapshot"}
	}
	defer db.Close()

	rows, rc, err := db.QueryRC("PRAGMA journal_mode")
	if err != nil {
		return connectionResult{state: stateForRC(rc), message: "journal-mode probe failed"}
	}
	mode := "unknown"
	if len(rows) > 0 {
		mode = String(rows[0], "journal_mode")
	}

	if checkTruncation {
		if st, statErr := os.Stat(dbPath); statErr == nil {
			if pc, perr := db.Query("PRAGMA page_count"); perr == nil && len(pc) > 0 {
				if ps, perr2 := db.Query("PRAGMA page_size"); perr2 == nil && len(ps) > 0 {
					declared := Int64(pc[0], "page_count") * Int64(ps[0], "page_size")
					if st.Size() < declared {
						return connectionResult{
							state:       LedgerCorrupt,
							journalMode: mode,
							message:     "database file is truncated",
						}
					}
				}
			}
		}
	}

	qc, rc, err := db.QueryRC("PRAGMA quick_check")
	if err != nil {
		return connectionResult{state: stateForCheckRC(rc), journalMode: mode, message: "SQLite quick_check failed"}
	}
	ok := true
	var errs []string
	for _, row := range qc {
		for _, v := range row {
			s := fmt.Sprint(v)
			if s != "ok" {
				ok = false
				errs = append(errs, s)
			}
		}
	}
	if ok {
		return connectionResult{state: Healthy, journalMode: mode, quickCheckOK: true}
	}
	return connectionResult{state: LedgerIntegrityFailed, journalMode: mode, errs: errs, message: "SQLite quick_check reported integrity errors"}
}

// diagnoseSnapshot runs the full two-phase diagnosis against the private
// snapshot: first with all files present, then — only when transient sidecars
// could explain a failure — against the main database alone. If the database
// alone is healthy, the failure is attributed to the sidecars.
func diagnoseSnapshot(snap *diagnosticSnapshot, sqliteVersion string) Diagnosis {
	full := diagnoseConnection(snap.dbPath, !snap.walExists)
	if full.state == Healthy {
		return buildDiagnosis(Healthy, full, snap, sqliteVersion)
	}
	if snap.walExists || snap.shmExists {
		if err := snap.moveSidecarsAside(); err != nil {
			return inconclusive("cannot isolate transient sidecars for diagnosis", sqliteVersion)
		}
		main := diagnoseConnection(snap.dbPath, false)
		if main.state == Healthy {
			d := buildDiagnosis(WALInconsistent, full, snap, sqliteVersion)
			d.Message = "WAL/SHM is inconsistent: SQLite fails against the full snapshot but the database alone is healthy"
			return d
		}
		return buildDiagnosis(main.state, main, snap, sqliteVersion)
	}
	return buildDiagnosis(full.state, full, snap, sqliteVersion)
}

func buildDiagnosis(state DiagnosisState, r connectionResult, snap *diagnosticSnapshot, sqliteVersion string) Diagnosis {
	d := Diagnosis{
		State:         state,
		ReasonCode:    reasonCode(state),
		Message:       r.message,
		SQLiteVersion: sqliteVersion,
		JournalMode:   r.journalMode,
		WALPresent:    snap.walExists,
		SHMPresent:    snap.shmExists,
		QuickCheckOK:  r.quickCheckOK,
	}
	if len(r.errs) > 0 {
		d.IntegrityErrors = boundedIntegrityErrors(r.errs)
	}
	if d.Message == "" {
		d.Message = defaultMessage(state)
	}
	return d
}

const maxIntegrityErrors = 8

func boundedIntegrityErrors(errs []string) []string {
	out := make([]string, 0, len(errs))
	for i, e := range errs {
		if i >= maxIntegrityErrors {
			break
		}
		if len(e) > 200 {
			e = e[:200] + "..."
		}
		out = append(out, e)
	}
	return out
}
