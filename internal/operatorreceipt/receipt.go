package operatorreceipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

const Version = "0.1"

type Receipt struct {
	Version        string    `json:"version"`
	Sequence       int64     `json:"sequence"`
	Action         string    `json:"action"`
	Actor          string    `json:"actor"`
	Subject        string    `json:"subject"`
	Reason         string    `json:"reason,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	Digest         string    `json:"digest"`
	KeyID          string    `json:"key_id"`
	Approver       string    `json:"approver"`
	Signature      string    `json:"signature"`
}

type Verification struct {
	Directory  string    `json:"directory"`
	Valid      bool      `json:"valid"`
	Count      int       `json:"count"`
	HeadDigest string    `json:"head_digest,omitempty"`
	Findings   []string  `json:"findings,omitempty"`
	VerifiedAt time.Time `json:"verified_at"`
}

type unsignedReceipt struct {
	Version        string    `json:"version"`
	Sequence       int64     `json:"sequence"`
	Action         string    `json:"action"`
	Actor          string    `json:"actor"`
	Subject        string    `json:"subject"`
	Reason         string    `json:"reason,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	KeyID          string    `json:"key_id"`
	Approver       string    `json:"approver"`
}

func Record(dir string, key operatorapproval.PrivateKeyFile, action, actor, subject, reason string, now time.Time) (Receipt, error) {
	for label, value := range map[string]string{"action": action, "actor": actor, "subject": subject} {
		if strings.TrimSpace(value) == "" {
			return Receipt{}, fmt.Errorf("%s is required", label)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Receipt{}, err
	}
	receipts, err := loadReceipts(dir)
	if err != nil {
		return Receipt{}, err
	}
	seq := int64(len(receipts) + 1)
	prev := ""
	if len(receipts) > 0 {
		prev = receipts[len(receipts)-1].Digest
	}
	u := unsignedReceipt{Version: Version, Sequence: seq, Action: strings.TrimSpace(action), Actor: strings.TrimSpace(actor), Subject: strings.TrimSpace(subject), Reason: strings.TrimSpace(reason), OccurredAt: now.UTC().Truncate(time.Second), PreviousDigest: prev, KeyID: key.KeyID, Approver: key.Approver}
	canonical, _ := json.Marshal(u)
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	sig, err := operatorapproval.SignDetached(key, []byte(digest))
	if err != nil {
		return Receipt{}, err
	}
	r := Receipt{Version: u.Version, Sequence: u.Sequence, Action: u.Action, Actor: u.Actor, Subject: u.Subject, Reason: u.Reason, OccurredAt: u.OccurredAt, PreviousDigest: u.PreviousDigest, Digest: digest, KeyID: u.KeyID, Approver: u.Approver, Signature: sig}
	b, _ := json.MarshalIndent(r, "", "  ")
	path := filepath.Join(dir, fmt.Sprintf("%020d-%s.json", seq, digest[:12]))
	if _, err := os.Stat(path); err == nil {
		return Receipt{}, errors.New("receipt already exists")
	}
	if err := writeExclusive(path, append(b, '\n')); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func Verify(dir string, ring operatorapproval.Keyring, now time.Time) (Verification, error) {
	result := Verification{Directory: dir, Valid: true, VerifiedAt: now.UTC()}
	receipts, err := loadReceipts(dir)
	if err != nil {
		return result, err
	}
	prev := ""
	for i, r := range receipts {
		if r.Sequence != int64(i+1) {
			result.Findings = append(result.Findings, fmt.Sprintf("sequence gap at %d", i+1))
		}
		if r.PreviousDigest != prev {
			result.Findings = append(result.Findings, fmt.Sprintf("previous digest mismatch at sequence %d", r.Sequence))
		}
		u := unsignedReceipt{Version: r.Version, Sequence: r.Sequence, Action: r.Action, Actor: r.Actor, Subject: r.Subject, Reason: r.Reason, OccurredAt: r.OccurredAt, PreviousDigest: r.PreviousDigest, KeyID: r.KeyID, Approver: r.Approver}
		canonical, _ := json.Marshal(u)
		sum := sha256.Sum256(canonical)
		expected := hex.EncodeToString(sum[:])
		if expected != r.Digest {
			result.Findings = append(result.Findings, fmt.Sprintf("digest mismatch at sequence %d", r.Sequence))
		}
		if err := operatorapproval.VerifyDetached(ring, r.KeyID, r.Approver, r.Signature, []byte(r.Digest)); err != nil {
			result.Findings = append(result.Findings, fmt.Sprintf("signature at sequence %d: %v", r.Sequence, err))
		}
		prev = r.Digest
	}
	result.Count = len(receipts)
	result.HeadDigest = prev
	result.Valid = len(result.Findings) == 0
	return result, nil
}

func loadReceipts(dir string) ([]Receipt, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]Receipt, 0, len(names))
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var r Receipt
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, r)
	}
	return out, nil
}
func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
