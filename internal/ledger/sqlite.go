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
	"os"
	"runtime"
	"strconv"
	"sync"
	"unsafe"
)

type FaultInjector interface {
	Before(operation string) error
}

type DB struct {
	mu     sync.Mutex
	db     *C.sqlite3
	faults FaultInjector
}

type Value any

type Row map[string]Value

func Open(path string) (*DB, error) { return OpenWithFaultInjector(path, nil) }

func OpenWithFaultInjector(path string, faults FaultInjector) (*DB, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var raw *C.sqlite3
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX
	if rc := C.sqlite3_open_v2(cpath, &raw, C.int(flags), nil); rc != C.SQLITE_OK {
		msg := "sqlite open failed"
		if raw != nil {
			msg = C.GoString(C.fd_sqlite_errmsg(raw))
			C.sqlite3_close(raw)
		}
		return nil, errors.New(msg)
	}
	db := &DB{db: raw, faults: faults}
	runtime.SetFinalizer(db, func(d *DB) { _ = d.Close() })
	if err := db.ExecScript("PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db == nil {
		return nil
	}
	rc := C.sqlite3_close(d.db)
	if rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite close: %s", C.GoString(C.fd_sqlite_errmsg(d.db)))
	}
	d.db = nil
	return nil
}

func (d *DB) before(operation string) error {
	if d.faults != nil {
		return d.faults.Before(operation)
	}
	return nil
}

func (d *DB) ExecScript(sql string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.before("exec_script"); err != nil {
		return err
	}
	return d.execScriptLocked(sql)
}

func (d *DB) execScriptLocked(sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))
	var errmsg *C.char
	if rc := C.sqlite3_exec(d.db, csql, nil, nil, &errmsg); rc != C.SQLITE_OK {
		defer C.sqlite3_free(unsafe.Pointer(errmsg))
		return fmt.Errorf("sqlite exec: %s", C.GoString(errmsg))
	}
	return nil
}

func (d *DB) WithTx(fn func(*Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.before("begin"); err != nil {
		return err
	}
	if err := d.execScriptLocked("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	tx := &Tx{db: d}
	if err := fn(tx); err != nil {
		_ = d.execScriptLocked("ROLLBACK")
		return err
	}
	if err := d.before("commit"); err != nil {
		_ = d.execScriptLocked("ROLLBACK")
		return err
	}
	if err := d.execScriptLocked("COMMIT"); err != nil {
		_ = d.execScriptLocked("ROLLBACK")
		return err
	}
	return nil
}

type Tx struct{ db *DB }

func (d *DB) Exec(query string, args ...Value) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.before("exec"); err != nil {
		return 0, err
	}
	return execPrepared(d.db, query, args...)
}
func (t *Tx) Exec(query string, args ...Value) (int64, error) {
	return execPrepared(t.db.db, query, args...)
}
func (d *DB) Query(query string, args ...Value) ([]Row, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.before("query"); err != nil {
		return nil, err
	}
	return queryPrepared(d.db, query, args...)
}
func (t *Tx) Query(query string, args ...Value) ([]Row, error) {
	return queryPrepared(t.db.db, query, args...)
}
func (d *DB) QueryOne(query string, args ...Value) (Row, error) {
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}
func (t *Tx) QueryOne(query string, args ...Value) (Row, error) {
	rows, err := t.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}

var ErrNotFound = errors.New("not found")

func prepare(db *C.sqlite3, query string) (*C.sqlite3_stmt, error) {
	cq := C.CString(query)
	defer C.free(unsafe.Pointer(cq))
	var stmt *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(db, cq, -1, &stmt, nil); rc != C.SQLITE_OK {
		return nil, fmt.Errorf("sqlite prepare: %s", C.GoString(C.fd_sqlite_errmsg(db)))
	}
	return stmt, nil
}

func bind(stmt *C.sqlite3_stmt, args []Value) error {
	for i, arg := range args {
		idx := C.int(i + 1)
		var rc C.int
		switch v := arg.(type) {
		case nil:
			rc = C.sqlite3_bind_null(stmt, idx)
		case string:
			cs := C.CString(v)
			rc = C.sqlite3_bind_text(stmt, idx, cs, C.int(len(v)), (*[0]byte)(C.free))
		case []byte:
			if len(v) == 0 {
				rc = C.sqlite3_bind_blob(stmt, idx, nil, 0, nil)
			} else {
				rc = C.sqlite3_bind_blob(stmt, idx, unsafe.Pointer(&v[0]), C.int(len(v)), C.SQLITE_TRANSIENT)
			}
		case int:
			rc = C.sqlite3_bind_int64(stmt, idx, C.sqlite3_int64(v))
		case int64:
			rc = C.sqlite3_bind_int64(stmt, idx, C.sqlite3_int64(v))
		case bool:
			iv := 0
			if v {
				iv = 1
			}
			rc = C.sqlite3_bind_int(stmt, idx, C.int(iv))
		case float64:
			rc = C.sqlite3_bind_double(stmt, idx, C.double(v))
		default:
			return fmt.Errorf("unsupported sqlite bind type %T", arg)
		}
		if rc != C.SQLITE_OK {
			return fmt.Errorf("sqlite bind argument %d failed", i+1)
		}
	}
	return nil
}

func execPrepared(db *C.sqlite3, query string, args ...Value) (int64, error) {
	stmt, err := prepare(db, query)
	if err != nil {
		return 0, err
	}
	defer C.sqlite3_finalize(stmt)
	if err := bind(stmt, args); err != nil {
		return 0, err
	}
	if rc := C.sqlite3_step(stmt); rc != C.SQLITE_DONE {
		return 0, fmt.Errorf("sqlite step: %s", C.GoString(C.fd_sqlite_errmsg(db)))
	}
	return int64(C.sqlite3_changes(db)), nil
}

func queryPrepared(db *C.sqlite3, query string, args ...Value) ([]Row, error) {
	stmt, err := prepare(db, query)
	if err != nil {
		return nil, err
	}
	defer C.sqlite3_finalize(stmt)
	if err := bind(stmt, args); err != nil {
		return nil, err
	}
	var rows []Row
	for {
		rc := C.sqlite3_step(stmt)
		if rc == C.SQLITE_DONE {
			return rows, nil
		}
		if rc != C.SQLITE_ROW {
			return nil, fmt.Errorf("sqlite query step: %s", C.GoString(C.fd_sqlite_errmsg(db)))
		}
		n := int(C.sqlite3_column_count(stmt))
		row := make(Row, n)
		for i := 0; i < n; i++ {
			name := C.GoString(C.sqlite3_column_name(stmt, C.int(i)))
			switch C.sqlite3_column_type(stmt, C.int(i)) {
			case C.SQLITE_INTEGER:
				row[name] = int64(C.sqlite3_column_int64(stmt, C.int(i)))
			case C.SQLITE_FLOAT:
				row[name] = float64(C.sqlite3_column_double(stmt, C.int(i)))
			case C.SQLITE_TEXT:
				p := C.sqlite3_column_text(stmt, C.int(i))
				row[name] = C.GoString((*C.char)(unsafe.Pointer(p)))
			case C.SQLITE_BLOB:
				p := C.sqlite3_column_blob(stmt, C.int(i))
				l := int(C.sqlite3_column_bytes(stmt, C.int(i)))
				if l > 0 {
					row[name] = C.GoBytes(p, C.int(l))
				} else {
					row[name] = []byte{}
				}
			default:
				row[name] = nil
			}
		}
		rows = append(rows, row)
	}
}

func String(row Row, key string) string {
	if v := row[key]; v != nil {
		return fmt.Sprint(v)
	}
	return ""
}
func Int64(row Row, key string) int64 {
	switch v := row[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	}
	return 0
}

// IntegrityCheck runs SQLite's full integrity check and returns an error unless every result is "ok".
func (d *DB) IntegrityCheck() error {
	rows, err := d.Query("PRAGMA integrity_check")
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("sqlite integrity_check returned no rows")
	}
	for _, row := range rows {
		for _, value := range row {
			if fmt.Sprint(value) != "ok" {
				return fmt.Errorf("sqlite integrity check failed: %v", value)
			}
		}
	}
	return nil
}

// Checkpoint requests a blocking WAL checkpoint so backup artifacts contain all committed data.
func (d *DB) Checkpoint() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.before("checkpoint"); err != nil {
		return err
	}
	if d.db == nil {
		return errors.New("sqlite database is closed")
	}
	var logFrames, checkpointed C.int
	rc := C.sqlite3_wal_checkpoint_v2(d.db, nil, C.SQLITE_CHECKPOINT_FULL, &logFrames, &checkpointed)
	if rc != C.SQLITE_OK {
		return fmt.Errorf("sqlite WAL checkpoint: %s", C.GoString(C.fd_sqlite_errmsg(d.db)))
	}
	return nil
}

// BackupTo creates a consistent SQLite backup using the online backup API.
func (d *DB) BackupTo(path string) error {
	if err := d.before("backup"); err != nil {
		return err
	}
	if path == "" {
		return errors.New("backup path is required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db == nil {
		return errors.New("sqlite database is closed")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var target *C.sqlite3
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX
	if rc := C.sqlite3_open_v2(cpath, &target, C.int(flags), nil); rc != C.SQLITE_OK {
		if target != nil {
			defer C.sqlite3_close(target)
			return fmt.Errorf("sqlite backup target open: %s", C.GoString(C.fd_sqlite_errmsg(target)))
		}
		return errors.New("sqlite backup target open failed")
	}
	defer C.sqlite3_close(target)
	mainName := C.CString("main")
	defer C.free(unsafe.Pointer(mainName))
	backup := C.sqlite3_backup_init(target, mainName, d.db, mainName)
	if backup == nil {
		return fmt.Errorf("sqlite backup init: %s", C.GoString(C.fd_sqlite_errmsg(target)))
	}
	rc := C.sqlite3_backup_step(backup, -1)
	finishRC := C.sqlite3_backup_finish(backup)
	if rc != C.SQLITE_DONE {
		return fmt.Errorf("sqlite backup step failed: rc=%d", int(rc))
	}
	if finishRC != C.SQLITE_OK {
		return fmt.Errorf("sqlite backup finish: %s", C.GoString(C.fd_sqlite_errmsg(target)))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return nil
}
