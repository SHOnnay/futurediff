package threatmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

const Version = "0.1"

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
)

type Check struct {
	ThreatID string `json:"threat_id"`
	Title    string `json:"title"`
	Status   Status `json:"status"`
	Evidence string `json:"evidence"`
}
type Report struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Checks      []Check   `json:"checks"`
	Passed      int       `json:"passed"`
	Failed      int       `json:"failed"`
	Secure      bool      `json:"secure"`
	Digest      string    `json:"digest"`
}

func Run(now time.Time) (Report, error) {
	checks := []Check{
		checkAgentAuthority(),
		checkEgress(),
		checkMaintenance(now),
		checkEvidence(now),
		checkApproval(now),
		checkTerminalStates(),
		checkKeyRevocation(now),
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].ThreatID < checks[j].ThreatID })
	r := Report{Version: Version, GeneratedAt: now.UTC(), Checks: checks}
	for _, c := range checks {
		if c.Status == Pass {
			r.Passed++
		} else {
			r.Failed++
		}
	}
	r.Secure = r.Failed == 0
	r.Digest = digest(r)
	return r, nil
}

func checkAgentAuthority() Check {
	privileged := map[string]bool{"approval_material": true, "transaction_approve": true, "transaction_commit": true, "transaction_recover": true, "transaction_abort": true}
	for _, e := range apicontract.Current().Endpoints {
		if privileged[e.OperationID] && e.AgentSafe {
			return fail("TM-001", "Agent authority segregation", fmt.Sprintf("privileged operation %s is agent-safe", e.OperationID))
		}
	}
	return pass("TM-001", "Agent authority segregation", "approval, commit, recovery, and abort operations are not agent-safe")
}
func checkEgress() Check {
	rule, err := egress.RuleFromBase("https://api.github.com/repos", http.MethodGet, http.MethodPost)
	if err != nil {
		return fail("TM-002", "Provider egress confinement", err.Error())
	}
	tr, err := egress.NewTransport(egress.Policy{Rules: []egress.Rule{rule}})
	if err != nil {
		return fail("TM-002", "Provider egress confinement", err.Error())
	}
	good, _ := http.NewRequest(http.MethodPost, "https://api.github.com/repos/acme/app/pulls", nil)
	bad, _ := http.NewRequest(http.MethodPost, "https://api.github.com.evil.test/repos/acme/app/pulls", nil)
	badPath, _ := http.NewRequest(http.MethodPost, "https://api.github.com/repos-evil/acme/app", nil)
	if tr.ValidateRequest(good) != nil || tr.ValidateRequest(bad) == nil || tr.ValidateRequest(badPath) == nil {
		return fail("TM-002", "Provider egress confinement", "exact host/path policy did not fail closed")
	}
	return pass("TM-002", "Provider egress confinement", "exact provider host and path accepted; look-alikes rejected")
}
func checkMaintenance(now time.Time) Check {
	dir, err := os.MkdirTemp("", "fd-threat-maint-*")
	if err != nil {
		return fail("TM-003", "Maintenance mutation freeze", err.Error())
	}
	defer os.RemoveAll(dir)
	m := &maintenance.Manager{Path: filepath.Join(dir, "maintenance.json")}
	if _, err := m.Enable("security drill", "threat-suite", time.Hour, now); err != nil {
		return fail("TM-003", "Maintenance mutation freeze", err.Error())
	}
	allowed, _, err := m.MutationsAllowed(now)
	if err != nil || allowed {
		return fail("TM-003", "Maintenance mutation freeze", "mutations remained allowed")
	}
	if err := os.WriteFile(m.Path, []byte(`{"version":"0.1","enabled":false,"digest":"forged"}`), 0o600); err != nil {
		return fail("TM-003", "Maintenance mutation freeze", err.Error())
	}
	if _, err := m.Status(now); err == nil {
		return fail("TM-003", "Maintenance mutation freeze", "forged maintenance state was accepted")
	}
	return pass("TM-003", "Maintenance mutation freeze", "maintenance blocks mutations and detects state tampering")
}
func checkEvidence(now time.Time) Check {
	dir, err := os.MkdirTemp("", "fd-threat-evidence-*")
	if err != nil {
		return fail("TM-004", "Evidence confidentiality and integrity", err.Error())
	}
	defer os.RemoveAll(dir)
	k, err := evidencecrypto.Generate(now)
	if err != nil {
		return fail("TM-004", "Evidence confidentiality and integrity", err.Error())
	}
	path := filepath.Join(dir, "key.json")
	if err := evidencecrypto.WriteKey(path, k); err != nil {
		return fail("TM-004", "Evidence confidentiality and integrity", err.Error())
	}
	c, err := evidencecrypto.Load(path)
	if err != nil {
		return fail("TM-004", "Evidence confidentiality and integrity", err.Error())
	}
	aad := []byte("tx:exec:stdout")
	enc, err := c.Seal([]byte("sensitive-output"), aad)
	if err != nil {
		return fail("TM-004", "Evidence confidentiality and integrity", err.Error())
	}
	if strings.Contains(string(enc), "sensitive-output") {
		return fail("TM-004", "Evidence confidentiality and integrity", "plaintext appeared in ciphertext")
	}
	if _, err := c.Open(enc, []byte("wrong")); err == nil {
		return fail("TM-004", "Evidence confidentiality and integrity", "wrong associated data accepted")
	}
	enc[len(enc)-1] ^= 1
	if _, err := c.Open(enc, aad); err == nil {
		return fail("TM-004", "Evidence confidentiality and integrity", "tampered ciphertext accepted")
	}
	return pass("TM-004", "Evidence confidentiality and integrity", "AES-GCM hides plaintext and rejects AAD/ciphertext tampering")
}
func checkApproval(now time.Time) Check {
	priv, pub, err := operatorapproval.Generate("operator@example.com", now)
	if err != nil {
		return fail("TM-005", "Approval authenticity and expiry", err.Error())
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	env, err := operatorapproval.Sign(priv, "tx", "digest", time.Minute, now)
	if err != nil {
		return fail("TM-005", "Approval authenticity and expiry", err.Error())
	}
	if err := operatorapproval.Verify(ring, env, "tx", "digest", now.Add(30*time.Second)); err != nil {
		return fail("TM-005", "Approval authenticity and expiry", err.Error())
	}
	tampered := env
	tampered.TransactionDigest = "changed"
	if operatorapproval.Verify(ring, tampered, "tx", "changed", now.Add(30*time.Second)) == nil {
		return fail("TM-005", "Approval authenticity and expiry", "tampered envelope accepted")
	}
	if operatorapproval.Verify(ring, env, "tx", "digest", now.Add(2*time.Minute)) == nil {
		return fail("TM-005", "Approval authenticity and expiry", "expired envelope accepted")
	}
	return pass("TM-005", "Approval authenticity and expiry", "valid signatures pass; tampered and expired envelopes fail")
}
func checkTerminalStates() Check {
	terminal := []domain.TransactionState{domain.StateCommitted, domain.StateAborted, domain.StateCompensated, domain.StateManualIntervention}
	all := []domain.TransactionState{domain.StateCreated, domain.StateActive, domain.StateSealed, domain.StateVerifying, domain.StateFailedVerification, domain.StateReady, domain.StateStale, domain.StateCommitting, domain.StateAborting, domain.StateAborted, domain.StateCompensating, domain.StateCompensated, domain.StateNeedsReconciliation, domain.StateCommitted, domain.StateManualIntervention}
	for _, from := range terminal {
		for _, to := range all {
			if domain.CanTransition(from, to) {
				return fail("TM-006", "Terminal state immutability", fmt.Sprintf("%s can transition to %s", from, to))
			}
		}
	}
	return pass("TM-006", "Terminal state immutability", "terminal transaction states have no outgoing transitions")
}
func checkKeyRevocation(now time.Time) Check {
	oldPriv, oldPub, err := operatorapproval.Generate("rotate@example.com", now)
	if err != nil {
		return fail("TM-007", "Approval key revocation", err.Error())
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{oldPub}}
	ring, _, newPub, err := operatorapproval.Rotate(ring, "rotate@example.com", false, now.Add(time.Second))
	if err != nil {
		return fail("TM-007", "Approval key revocation", err.Error())
	}
	ring, err = operatorapproval.SetEnabled(ring, oldPub.KeyID, false, false)
	if err != nil {
		return fail("TM-007", "Approval key revocation", err.Error())
	}
	env, _ := operatorapproval.Sign(oldPriv, "tx", "digest", time.Hour, now)
	if operatorapproval.Verify(ring, env, "tx", "digest", now.Add(time.Minute)) == nil {
		return fail("TM-007", "Approval key revocation", "disabled key remained trusted")
	}
	if _, err := operatorapproval.SetEnabled(ring, newPub.KeyID, false, false); err == nil {
		return fail("TM-007", "Approval key revocation", "final enabled key could be disabled without override")
	}
	return pass("TM-007", "Approval key revocation", "disabled keys are rejected and lockout is prevented by default")
}
func pass(id, title, evidence string) Check {
	return Check{ThreatID: id, Title: title, Status: Pass, Evidence: evidence}
}
func fail(id, title, evidence string) Check {
	return Check{ThreatID: id, Title: title, Status: Fail, Evidence: evidence}
}
func digest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
