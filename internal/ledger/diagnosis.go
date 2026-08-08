package ledger

/*
#cgo LDFLAGS: -lsqlite3
#include <stdlib.h>
#include <sqlite3.h>

static const char* fd_sqlite_errmsg(sqlite3 *db) { return sqlite3_errmsg(db); }
*/
import "C"

import (
	"crypto/sha256"
	"encoding/hex"
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

// maxDiagnoseTotalBytes bounds the aggregate size of the diagnostic snapshot
// (ledger.db plus any -wal/-shm sidecars). Each file is additionally bounded
// by maxDiagnoseBytes; the total bound protects the temporary filesystem from
// a quiescent but enormous ledger.
const maxDiagnoseTotalBytes = 256 << 20

const (
	diagnoseFileMode = 0o600
)

// Snapshot-establishment failures that are not storage I/O failures. They are
// reported as diagnosis_inconclusive with a nil error; only storage errors
// (errors.Is against ENOSPC/EDQUOT/EROFS/os.ErrPermission/EIO) are wrapped
// and returned so callers can classify them without parsing messages.
var (
	errSnapshotNonRegular     = errors.New("authoritative ledger file is not a regular file")
	errSnapshotOversized      = errors.New("file exceeds bounded diagnostic size")
	errSnapshotTotalTooLarge  = errors.New("ledger snapshot exceeds bounded total size")
	errSnapshotContentChanged = errors.New("authoritative ledger content changed during snapshot establishment")
	errSnapshotReplaced       = errors.New("authoritative ledger file was replaced during snapshot establishment")
	errSnapshotSetChanged     = errors.New("authoritative ledger file set changed during snapshot establishment")
)

// snapshotCopyStage identifies which authoritative file's copy is being
// verified when the narrow test hook fires.
type snapshotCopyStage int

const (
	stageAfterCopyDB snapshotCopyStage = iota
	stageAfterCopyWAL
	stageAfterCopySHM
)

// testSnapshotHook is a test-only hook invoked inside snapshot establishment
// after an authoritative file's bytes have been copied to the snapshot but
// before its stability verification completes. Tests use it to mutate the
// authoritative file set (or the copy) at a deterministic stage — never a
// timing-only race. It is nil in production.
var testSnapshotHook func(stage snapshotCopyStage, snap *diagnosticSnapshot)

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
	FullIntegrityOK bool           `json:"full_integrity_ok,omitempty"`
	IntegrityErrors []string       `json:"integrity_errors,omitempty"`
}

// DiagnoseOptions carries the caller's contract for a diagnosis.
type DiagnoseOptions struct {
	// Quiescent must be true only after the caller has proven that the
	// ledger daemon is stopped or the ledger is otherwise quiescent: no
	// writer may be active on the authoritative ledger.db, -wal, or -shm
	// files for the duration of the diagnosis. Diagnose itself performs no
	// liveness check and never claims to have proven quiescence. With
	// Quiescent=false, Diagnose fails closed as diagnosis_inconclusive
	// before reading or copying any authoritative file.
	Quiescent bool
	// FullIntegrity requests the full PRAGMA integrity_check on the private
	// coherent snapshot in addition to the routine quick_check. It is used
	// by opt-in fail-closed startup gates (futurediffd --require-integrity);
	// routine diagnosis (doctor) keeps the bounded quick check. Non-ok
	// results classify as LedgerIntegrityFailed, never corruption, and the
	// check always runs against the snapshot copy, never the authoritative
	// files.
	FullIntegrity bool
	// SnapshotTempDir, when non-empty, is the parent directory under which
	// the private diagnostic snapshot directory is created. Production
	// callers leave it empty: the snapshot is created with a unique private
	// 0700 name directly under the system temporary directory. Tests use it
	// to confine snapshot directories to a test-owned parent so cleanup and
	// leak assertions can never observe directories belonging to another
	// test, package, or process. It is purely a placement control for the
	// disposable snapshot; it never affects which authoritative files are
	// inspected, how they are copied, or what the diagnosis may touch.
	SnapshotTempDir string
}

// Diagnose performs a bounded, non-mutating integrity diagnosis of a ledger
// database without going through the normal read-write repository path.
//
// The authoritative ledger.db, -wal, and -shm files are never created,
// deleted, renamed, truncated, checkpointed, or repaired. Diagnosis runs
// against a private snapshot copy in a temporary directory, which is removed
// before returning. Snapshot establishment is the caller-facing contract: it
// refuses symlinks and every non-regular file kind, copies through a
// descriptor opened without following symlinks, verifies the recorded file
// identity and a source-before == copied == source-after hash chain, and
// revalidates the authoritative file set (appear, disappear, replace)
// afterwards. Any violation fails closed as diagnosis_inconclusive.
//
// The caller must assert quiescence through DiagnoseOptions; the function
// does not prove it and never claims to have. A missing ledger.db is
// DatabaseNotInitialized, never corruption. A missing -wal or -shm sidecar
// is never corruption on its own: SHM is transient WAL-index state, not
// authoritative durable state. Storage-capacity, permission, quota, and I/O
// errors are StorageIOFailure, never corruption. Anything that cannot be
// established conclusively fails closed.
//
// The returned error is non-nil only for storage I/O failures (wrapped so
// errors.Is works) or snapshot-cleanup failures; all other outcomes are
// expressed through Diagnosis.State.
func Diagnose(path string, opts DiagnoseOptions) (Diagnosis, error) {
	sqliteVersion := SQLiteVersion()
	if path == "" {
		return inconclusive("database path required", sqliteVersion), nil
	}
	if !opts.Quiescent {
		return inconclusive("caller must establish ledger quiescence (daemon stopped or ledger otherwise idle) before diagnosis; Quiescent was not set", sqliteVersion), nil
	}

	before, err := statAuthoritative(path)
	if err != nil {
		return storageFailure(fmt.Errorf("inspect ledger path: %w", err), sqliteVersion)
	}
	if !before.db.exists {
		return notInitialized(before, sqliteVersion), nil
	}
	if !before.db.regular {
		return inconclusive("authoritative ledger database is not a regular file", sqliteVersion), nil
	}
	if before.db.size > maxDiagnoseBytes {
		return inconclusive("database exceeds bounded diagnostic size", sqliteVersion), nil
	}
	if before.wal.exists && !before.wal.regular {
		return inconclusive("authoritative ledger WAL is not a regular file", sqliteVersion), nil
	}
	if before.wal.exists && before.wal.size > maxDiagnoseBytes {
		return inconclusive("WAL exceeds bounded diagnostic size", sqliteVersion), nil
	}
	if before.shm.exists && !before.shm.regular {
		return inconclusive("authoritative ledger SHM is not a regular file", sqliteVersion), nil
	}
	if before.shm.exists && before.shm.size > maxDiagnoseBytes {
		return inconclusive("SHM exceeds bounded diagnostic size", sqliteVersion), nil
	}
	if before.db.size+before.wal.size+before.shm.size > maxDiagnoseTotalBytes {
		return inconclusive("ledger snapshot exceeds bounded total size", sqliteVersion), nil
	}

	snap, err := createDiagnosticSnapshot(path, before, opts.SnapshotTempDir)
	if err != nil {
		if isStorageError(err) {
			return storageFailure(fmt.Errorf("create diagnostic snapshot: %w", err), sqliteVersion)
		}
		return inconclusive(snapshotFailureMessage(err), sqliteVersion), nil
	}

	d := diagnoseSnapshot(snap, sqliteVersion, opts.FullIntegrity)

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

// snapshotFailureMessage maps a snapshot-establishment failure to a
// caller-facing message. Storage errors never reach here; they are classified
// by isStorageError and returned wrapped.
func snapshotFailureMessage(err error) string {
	switch {
	case errors.Is(err, errSnapshotNonRegular):
		return "authoritative ledger file is not a regular file"
	case errors.Is(err, errSnapshotOversized):
		return "file exceeds bounded diagnostic size"
	case errors.Is(err, errSnapshotTotalTooLarge):
		return "ledger snapshot exceeds bounded total size"
	case errors.Is(err, errSnapshotContentChanged):
		return "authoritative ledger content changed while establishing the snapshot"
	case errors.Is(err, errSnapshotReplaced):
		return "authoritative ledger file was replaced while establishing the snapshot"
	case errors.Is(err, errSnapshotSetChanged):
		return "authoritative ledger file set changed while establishing the snapshot"
	default:
		return "cannot establish a coherent diagnostic snapshot"
	}
}

// fileIdentity is the stable (device, inode) identity of an authoritative
// file, recorded before snapshot establishment and revalidated afterwards.
type fileIdentity struct {
	dev uint64
	ino uint64
}

// authoritativeFile is the stat view of one authoritative ledger file.
type authoritativeFile struct {
	exists  bool
	regular bool
	size    int64
	modTime time.Time
	ident   fileIdentity
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
		ident:   statIdentity(fi),
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

// setEquals reports whether the authoritative file set is unchanged: every
// path has the same presence, and every file that existed still points at
// the same regular inode of the same size. Any difference means a file
// appeared, disappeared, or was replaced during snapshot establishment.
func (a authoritativeState) setEquals(b authoritativeState) bool {
	return sameSetFile(a.db, b.db) && sameSetFile(a.wal, b.wal) && sameSetFile(a.shm, b.shm)
}

func sameSetFile(a, b authoritativeFile) bool {
	if a.exists != b.exists {
		return false
	}
	if !a.exists {
		return true
	}
	return a.regular == b.regular && a.ident == b.ident && a.size == b.size
}

// diagnosticSnapshot is a private, disposable copy of the authoritative
// ledger files. SQLite diagnosis runs exclusively against these copies.
type diagnosticSnapshot struct {
	dir        string
	dbPath     string
	walPath    string
	shmPath    string
	walExists  bool
	shmExists  bool
	totalBytes int64
}

// createDiagnosticSnapshot copies the authoritative ledger files into a
// private disposable directory. tempParent is the parent for that directory;
// an empty tempParent selects the system temporary directory (the production
// default). The directory is always created with a unique 0700 name, and it
// is removed on every error path by fail and on success by snap.cleanup.
func createDiagnosticSnapshot(path string, before authoritativeState, tempParent string) (*diagnosticSnapshot, error) {
	if tempParent == "" {
		tempParent = os.TempDir()
	}
	dir, err := os.MkdirTemp(tempParent, "futurediff-diagnose-")
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

	if err := snap.copyAuthoritative(path, snap.dbPath, stageAfterCopyDB); err != nil {
		return fail(err)
	}
	if before.wal.exists {
		if err := snap.copyAuthoritative(path+"-wal", snap.walPath, stageAfterCopyWAL); err != nil {
			return fail(err)
		}
		snap.walExists = true
	}
	if before.shm.exists {
		if err := snap.copyAuthoritative(path+"-shm", snap.shmPath, stageAfterCopySHM); err != nil {
			return fail(err)
		}
		snap.shmExists = true
	}

	// The authoritative file set must be unchanged: nothing appeared,
	// disappeared, or was replaced while we copied.
	after, err := statAuthoritative(path)
	if err != nil {
		return fail(err)
	}
	if !before.setEquals(after) {
		return fail(errSnapshotSetChanged)
	}
	return snap, nil
}

// copyAuthoritative copies one authoritative file into the snapshot without
// following symlinks, verifying that the source is a stable regular file
// before, during, and after the copy. The recorded identity and the hash
// chain source-before == copied == source-after must all hold; any violation
// fails the snapshot. The stage hook (if any) fires after the bytes are
// copied and before stability verification, giving tests a deterministic
// point at which to mutate the authoritative file set.
func (s *diagnosticSnapshot) copyAuthoritative(src, dst string, stage snapshotCopyStage) error {
	// Lstat first: reject anything that is not a regular file before any
	// open, so a FIFO or device can never block the open or be read.
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errSnapshotNonRegular
	}
	if fi.Size() > maxDiagnoseBytes {
		return errSnapshotOversized
	}
	if s.totalBytes+fi.Size() > maxDiagnoseTotalBytes {
		return errSnapshotTotalTooLarge
	}

	// Open without following symlinks. The Lstat already rejected symlinks,
	// but a concurrent swap to a symlink would otherwise be followed here;
	// O_NOFOLLOW makes the open fail instead. O_NONBLOCK keeps a swapped-in
	// FIFO from blocking the open; the descriptor is verified below.
	in, err := openNoFollow(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// The descriptor must still be a regular file: the path may have been
	// swapped for a non-symlink kind between Lstat and open.
	if dfi, err := in.Stat(); err != nil {
		return err
	} else if !dfi.Mode().IsRegular() {
		return errSnapshotNonRegular
	}

	// Hash the source content before copying.
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	beforeHash, err := hashReader(in)
	if err != nil {
		return err
	}

	// Copy through the opened descriptor.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, diagnoseFileMode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, maxDiagnoseBytes+1))
	if err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if n > maxDiagnoseBytes {
		return errSnapshotOversized
	}
	s.totalBytes += n
	if s.totalBytes > maxDiagnoseTotalBytes {
		return errSnapshotTotalTooLarge
	}

	// Narrow test hook: deterministic mutation point for tests.
	if testSnapshotHook != nil {
		testSnapshotHook(stage, s)
	}

	// Hash the copied file.
	copiedHash, err := hashFile(dst)
	if err != nil {
		return err
	}

	// Re-read and hash the source after copying.
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	afterHash, err := hashReader(in)
	if err != nil {
		return err
	}

	// Require source-before == copied == source-after.
	if beforeHash != copiedHash || copiedHash != afterHash {
		return errSnapshotContentChanged
	}

	// Revalidate the path identity after copying: the path must still point
	// at the same regular inode we opened, or the file was replaced.
	fiAfter, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fiAfter.Mode().IsRegular() || statIdentity(fiAfter) != statIdentity(fi) {
		return errSnapshotReplaced
	}
	return nil
}

// openNoFollow opens a path without following a final symlink and without
// blocking on a non-regular file kind.
func openNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// statIdentity extracts the stable (device, inode) identity from a FileInfo
// produced by Lstat.
func statIdentity(fi os.FileInfo) fileIdentity {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fileIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino)}
	}
	return fileIdentity{}
}

// hashFile returns the SHA-256 of a file's content, bounded by
// maxDiagnoseBytes.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashReader(f)
}

// hashReader returns the SHA-256 of up to maxDiagnoseBytes+1 bytes read from
// r, failing closed when the content exceeds the bound.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, maxDiagnoseBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxDiagnoseBytes {
		return "", errSnapshotOversized
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
	state           DiagnosisState
	journalMode     string
	quickCheckOK    bool
	fullIntegrityOK bool
	errs            []string
	message         string
}

// diagnoseConnection opens one diagnostic connection against dbPath and runs
// the bounded routine checks: journal-mode probe, (optionally) truncation
// check, and PRAGMA quick_check; when fullIntegrity is set it additionally
// runs the full PRAGMA integrity_check. Both checks run only against the
// private snapshot copy. It never runs a WAL checkpoint.
func diagnoseConnection(dbPath string, checkTruncation, fullIntegrity bool) connectionResult {
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

	check := "quick_check"
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
	quickPassed := ok
	// Full mode: quick_check passed, so run the exhaustive integrity_check on
	// the same private snapshot. Non-ok results classify as
	// LedgerIntegrityFailed (never corruption).
	if ok && fullIntegrity {
		check = "integrity_check"
		ic, rc, err := db.QueryRC("PRAGMA integrity_check")
		if err != nil {
			return connectionResult{state: stateForCheckRC(rc), journalMode: mode, message: "SQLite integrity_check failed"}
		}
		for _, row := range ic {
			for _, v := range row {
				s := fmt.Sprint(v)
				if s != "ok" {
					ok = false
					errs = append(errs, s)
				}
			}
		}
	}
	if ok {
		return connectionResult{state: Healthy, journalMode: mode, quickCheckOK: true, fullIntegrityOK: fullIntegrity}
	}
	return connectionResult{state: LedgerIntegrityFailed, journalMode: mode, quickCheckOK: quickPassed, errs: errs, message: "SQLite " + check + " reported integrity errors"}
}

// diagnoseSnapshot runs the full two-phase diagnosis against the private
// snapshot: first with all files present, then — only when transient sidecars
// could explain a failure — against the main database alone. If the database
// alone is healthy, the failure is attributed to the sidecars. fullIntegrity
// selects the exhaustive integrity_check in both phases.
func diagnoseSnapshot(snap *diagnosticSnapshot, sqliteVersion string, fullIntegrity bool) Diagnosis {
	full := diagnoseConnection(snap.dbPath, !snap.walExists, fullIntegrity)
	if full.state == Healthy {
		return buildDiagnosis(Healthy, full, snap, sqliteVersion)
	}
	if snap.walExists || snap.shmExists {
		if err := snap.moveSidecarsAside(); err != nil {
			return inconclusive("cannot isolate transient sidecars for diagnosis", sqliteVersion)
		}
		main := diagnoseConnection(snap.dbPath, false, fullIntegrity)
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
		State:           state,
		ReasonCode:      reasonCode(state),
		Message:         r.message,
		SQLiteVersion:   sqliteVersion,
		JournalMode:     r.journalMode,
		WALPresent:      snap.walExists,
		SHMPresent:      snap.shmExists,
		QuickCheckOK:    r.quickCheckOK,
		FullIntegrityOK: r.fullIntegrityOK,
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
