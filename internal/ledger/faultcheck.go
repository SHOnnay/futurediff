package ledger

import (
	"errors"
	"os"
	"path/filepath"
)

type FaultCheckReport struct {
	CommitRollback      bool `json:"commit_failure_rolls_back"`
	BackupAtomicity     bool `json:"backup_failure_preserves_existing_artifact"`
	CorruptionDetection bool `json:"corrupted_backup_is_rejected"`
}

type oneShotFaults map[string]int

func (f oneShotFaults) Before(operation string) error {
	if f[operation] > 0 {
		f[operation]--
		return errors.New("injected " + operation + " failure")
	}
	return nil
}

func RunFaultSelfTest(root string) (FaultCheckReport, error) {
	if root == "" {
		return FaultCheckReport{}, errors.New("fault-test root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return FaultCheckReport{}, err
	}
	report := FaultCheckReport{}

	faults := oneShotFaults{}
	db, err := OpenWithFaultInjector(filepath.Join(root, "commit.db"), faults)
	if err != nil {
		return report, err
	}
	if err := db.ExecScript("CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		_ = db.Close()
		return report, err
	}
	faults["commit"] = 1
	if err := db.WithTx(func(tx *Tx) error { _, e := tx.Exec("INSERT INTO items(value) VALUES(?)", "rollback"); return e }); err == nil {
		_ = db.Close()
		return report, errors.New("commit fault was not triggered")
	}
	rows, err := db.Query("SELECT COUNT(*) AS count FROM items")
	if err != nil {
		_ = db.Close()
		return report, err
	}
	report.CommitRollback = Int64(rows[0], "count") == 0
	_ = db.Close()

	existing := filepath.Join(root, "known-good.db")
	if err := os.WriteFile(existing, []byte("known-good"), 0o600); err != nil {
		return report, err
	}
	faults = oneShotFaults{"backup": 1}
	db, err = OpenWithFaultInjector(filepath.Join(root, "backup-source.db"), faults)
	if err != nil {
		return report, err
	}
	_ = db.BackupTo(existing + ".tmp")
	_ = db.Close()
	data, err := os.ReadFile(existing)
	if err != nil {
		return report, err
	}
	report.BackupAtomicity = string(data) == "known-good"

	source := filepath.Join(root, "corruption-source.db")
	db, err = Open(source)
	if err != nil {
		return report, err
	}
	if err := db.ExecScript("CREATE TABLE values_table(id INTEGER PRIMARY KEY); INSERT INTO values_table DEFAULT VALUES;"); err != nil {
		_ = db.Close()
		return report, err
	}
	corrupt := filepath.Join(root, "corrupt.db")
	if err := db.BackupTo(corrupt); err != nil {
		_ = db.Close()
		return report, err
	}
	_ = db.Close()
	data, err = os.ReadFile(corrupt)
	if err != nil {
		return report, err
	}
	if len(data) < 512 {
		return report, errors.New("fault-test backup unexpectedly small")
	}
	for i := 100; i < 140; i++ {
		data[i] ^= 0xff
	}
	if err := os.WriteFile(corrupt, data, 0o600); err != nil {
		return report, err
	}
	candidate, openErr := Open(corrupt)
	if openErr != nil {
		report.CorruptionDetection = true
	} else {
		report.CorruptionDetection = candidate.IntegrityCheck() != nil
		_ = candidate.Close()
	}
	if !report.CommitRollback || !report.BackupAtomicity || !report.CorruptionDetection {
		return report, errors.New("one or more fault checks failed")
	}
	return report, nil
}
