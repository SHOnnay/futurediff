package integritycheckpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/operatorreceipt"
)

const Version = "0.1"

type Checkpoint struct {
	Version              string                 `json:"version"`
	CheckpointID         string                 `json:"checkpoint_id"`
	CreatedAt            time.Time              `json:"created_at"`
	LedgerFile           string                 `json:"ledger_file"`
	LedgerSHA256         string                 `json:"ledger_sha256"`
	LedgerSizeBytes      int64                  `json:"ledger_size_bytes"`
	Health               ledger.Health          `json:"health"`
	EventChains          ledger.EventChainHeads `json:"event_chains"`
	APIAccessHead        string                 `json:"api_access_head,omitempty"`
	OperatorReceiptHead  string                 `json:"operator_receipt_head,omitempty"`
	OperatorReceiptCount int                    `json:"operator_receipt_count,omitempty"`
	MaterialDigest       string                 `json:"material_digest"`
	KeyID                string                 `json:"key_id"`
	Approver             string                 `json:"approver"`
	Signature            string                 `json:"signature"`
}
type unsigned struct {
	Version              string                 `json:"version"`
	CheckpointID         string                 `json:"checkpoint_id"`
	CreatedAt            time.Time              `json:"created_at"`
	LedgerFile           string                 `json:"ledger_file"`
	LedgerSHA256         string                 `json:"ledger_sha256"`
	LedgerSizeBytes      int64                  `json:"ledger_size_bytes"`
	Health               ledger.Health          `json:"health"`
	EventChains          ledger.EventChainHeads `json:"event_chains"`
	APIAccessHead        string                 `json:"api_access_head,omitempty"`
	OperatorReceiptHead  string                 `json:"operator_receipt_head,omitempty"`
	OperatorReceiptCount int                    `json:"operator_receipt_count,omitempty"`
	KeyID                string                 `json:"key_id"`
	Approver             string                 `json:"approver"`
}
type Verification struct {
	Valid      bool       `json:"valid"`
	Checkpoint Checkpoint `json:"checkpoint"`
	Findings   []string   `json:"findings,omitempty"`
	VerifiedAt time.Time  `json:"verified_at"`
}

func digestUnsigned(u unsigned) string {
	b, _ := json.Marshal(u)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func fileDigest(path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), int64(len(b)), nil
}
func Create(root, output, privatePath, keyringPath, receiptDir string, now time.Time) (Checkpoint, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(output) {
		return Checkpoint{}, errors.New("root and output must be absolute")
	}
	status, err := daemonlock.Inspect(filepath.Join(root, "daemon.lock"), now)
	if err != nil {
		return Checkpoint{}, err
	}
	if status.Held {
		return Checkpoint{}, errors.New("daemon must be stopped before creating an integrity checkpoint")
	}
	lock, err := daemonlock.Acquire(filepath.Join(root, "daemon.lock"), root, now)
	if err != nil {
		return Checkpoint{}, err
	}
	defer lock.Release()
	key, err := operatorapproval.LoadPrivate(privatePath)
	if err != nil {
		return Checkpoint{}, err
	}
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		return Checkpoint{}, err
	}
	defer repo.Close()
	audit, err := repo.Audit()
	if err != nil {
		return Checkpoint{}, err
	}
	if !audit.Healthy {
		return Checkpoint{}, errors.New("ledger audit is unhealthy")
	}
	base := strings.TrimSuffix(output, filepath.Ext(output))
	backupPath := base + ".ledger.db"
	backup, err := repo.Backup(backupPath)
	if err != nil {
		return Checkpoint{}, err
	}
	heads, err := repo.EventChainHeads()
	if err != nil {
		return Checkpoint{}, err
	}
	apiHead, err := repo.VerifyAPIAccessChain()
	if err != nil {
		return Checkpoint{}, err
	}
	receiptHead := ""
	receiptCount := 0
	if receiptDir != "" {
		ring, err := operatorapproval.LoadKeyring(keyringPath)
		if err != nil {
			return Checkpoint{}, err
		}
		v, err := operatorreceipt.Verify(receiptDir, ring, now)
		if err != nil {
			return Checkpoint{}, err
		}
		if !v.Valid {
			return Checkpoint{}, errors.New("operator receipt chain is invalid")
		}
		receiptHead = v.HeadDigest
		receiptCount = v.Count
	}
	u := unsigned{Version: Version, CheckpointID: domain.NewID("checkpoint"), CreatedAt: now.UTC().Truncate(time.Second), LedgerFile: filepath.Base(backup.Path), LedgerSHA256: backup.SHA256, LedgerSizeBytes: backup.SizeBytes, Health: audit.Health, EventChains: heads, APIAccessHead: apiHead, OperatorReceiptHead: receiptHead, OperatorReceiptCount: receiptCount, KeyID: key.KeyID, Approver: key.Approver}
	dig := digestUnsigned(u)
	sig, err := operatorapproval.SignDetached(key, []byte(dig))
	if err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{Version: u.Version, CheckpointID: u.CheckpointID, CreatedAt: u.CreatedAt, LedgerFile: u.LedgerFile, LedgerSHA256: u.LedgerSHA256, LedgerSizeBytes: u.LedgerSizeBytes, Health: u.Health, EventChains: u.EventChains, APIAccessHead: u.APIAccessHead, OperatorReceiptHead: u.OperatorReceiptHead, OperatorReceiptCount: u.OperatorReceiptCount, MaterialDigest: dig, KeyID: u.KeyID, Approver: u.Approver, Signature: sig}
	b, _ := json.MarshalIndent(cp, "", "  ")
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return Checkpoint{}, err
	}
	tmp := output + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return Checkpoint{}, err
	}
	if err := os.Rename(tmp, output); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}
func Load(path string) (Checkpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Checkpoint{}, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var c Checkpoint
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var x any
	if err := d.Decode(&x); err == nil {
		return c, errors.New("trailing JSON value rejected")
	}
	return c, nil
}
func Verify(checkpointPath, keyringPath, ledgerPath, receiptDir string, now time.Time) (Verification, error) {
	cp, err := Load(checkpointPath)
	if err != nil {
		return Verification{}, err
	}
	v := Verification{Checkpoint: cp, Valid: true, VerifiedAt: now.UTC()}
	if cp.Version != Version {
		v.Findings = append(v.Findings, "unsupported checkpoint version")
	}
	u := unsigned{Version: cp.Version, CheckpointID: cp.CheckpointID, CreatedAt: cp.CreatedAt, LedgerFile: cp.LedgerFile, LedgerSHA256: cp.LedgerSHA256, LedgerSizeBytes: cp.LedgerSizeBytes, Health: cp.Health, EventChains: cp.EventChains, APIAccessHead: cp.APIAccessHead, OperatorReceiptHead: cp.OperatorReceiptHead, OperatorReceiptCount: cp.OperatorReceiptCount, KeyID: cp.KeyID, Approver: cp.Approver}
	dig := digestUnsigned(u)
	if dig != cp.MaterialDigest {
		v.Findings = append(v.Findings, "checkpoint material digest mismatch")
	}
	ring, err := operatorapproval.LoadKeyring(keyringPath)
	if err != nil {
		return v, err
	}
	if err := operatorapproval.VerifyDetached(ring, cp.KeyID, cp.Approver, cp.Signature, []byte(cp.MaterialDigest)); err != nil {
		v.Findings = append(v.Findings, "signature: "+err.Error())
	}
	if ledgerPath == "" {
		ledgerPath = filepath.Join(filepath.Dir(checkpointPath), cp.LedgerFile)
	}
	sha, size, err := fileDigest(ledgerPath)
	if err != nil {
		return v, err
	}
	if sha != cp.LedgerSHA256 || size != cp.LedgerSizeBytes {
		v.Findings = append(v.Findings, "ledger backup digest or size mismatch")
	}
	repo, err := ledger.OpenRepository(ledgerPath)
	if err != nil {
		return v, err
	}
	defer repo.Close()
	audit, err := repo.Audit()
	if err != nil {
		return v, err
	}
	if !audit.Healthy {
		v.Findings = append(v.Findings, "ledger backup audit is unhealthy")
	}
	heads, err := repo.EventChainHeads()
	if err != nil {
		return v, err
	}
	if heads.Digest != cp.EventChains.Digest || heads.Count != cp.EventChains.Count {
		v.Findings = append(v.Findings, "event-chain heads mismatch")
	}
	api, err := repo.VerifyAPIAccessChain()
	if err != nil {
		v.Findings = append(v.Findings, "API access chain: "+err.Error())
	} else if api != cp.APIAccessHead {
		v.Findings = append(v.Findings, "API access head mismatch")
	}
	if cp.OperatorReceiptCount > 0 {
		rv, err := operatorreceipt.Verify(receiptDir, ring, now)
		if err != nil {
			return v, err
		}
		if !rv.Valid || rv.HeadDigest != cp.OperatorReceiptHead || rv.Count != cp.OperatorReceiptCount {
			v.Findings = append(v.Findings, "operator receipt chain mismatch")
		}
	}
	v.Valid = len(v.Findings) == 0
	if !v.Valid {
		return v, fmt.Errorf("integrity checkpoint verification failed")
	}
	return v, nil
}
