package authzconformance

import (
	"path/filepath"
	"reflect"
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/capability"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	Version      string  `json:"version"`
	PolicyDigest string  `json:"policy_digest"`
	Passed       int     `json:"passed"`
	Failed       int     `json:"failed"`
	Conformant   bool    `json:"conformant"`
	Checks       []Check `json:"checks"`
}

func Run(policy authorization.Policy, workDir string, now time.Time) Report {
	r := Report{Version: "1", Checks: []Check{}}
	add := func(id string, ok bool, message string) {
		status := "pass"
		if ok {
			r.Passed++
		} else {
			status = "fail"
			r.Failed++
		}
		r.Checks = append(r.Checks, Check{ID: id, Status: status, Message: message})
	}
	a, err := authorization.Compile(policy)
	add("policy_compiles", err == nil, message(err, "policy compiled"))
	if err != nil {
		r.Conformant = false
		return r
	}
	r.PolicyDigest = a.Digest()
	add("default_deny", policy.Default == "deny", "policy default is deny")
	contract := apicontract.Current()
	unknownDenied := true
	for _, e := range contract.Endpoints {
		if a.Decide(^uint32(0), e.OperationID).Allowed {
			unknownDenied = false
			break
		}
	}
	add("unbound_uid_denied", unknownDenied, "an unbound UID is denied for every operation")
	deterministic := true
	for _, b := range policy.Bindings {
		for _, e := range contract.Endpoints {
			if !reflect.DeepEqual(a.Decide(b.UID, e.OperationID), a.Decide(b.UID, e.OperationID)) {
				deterministic = false
			}
		}
	}
	add("deterministic_decisions", deterministic, "repeated decisions are identical")
	agentSafe := true
	for _, role := range policy.AgentRoles {
		if !a.IsAgentRole(role) {
			agentSafe = false
		}
	}
	add("agent_roles_declared", agentSafe, "agent roles are recognized and safe")

	priv, pub, keyErr := operatorapproval.Generate("conformance-operator", now)
	add("ephemeral_signing_key", keyErr == nil, message(keyErr, "ephemeral Ed25519 key generated"))
	if keyErr == nil {
		ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
		tok, signErr := capability.Sign(priv, 1000, "transaction_commit", "tx-test", time.Minute, now)
		add("capability_sign", signErr == nil, message(signErr, "scoped capability signed"))
		if signErr == nil {
			add("capability_valid", capability.Verify(ring, tok, 1000, "transaction_commit", "tx-test", now) == nil, "valid capability accepted")
			add("capability_uid_bound", capability.Verify(ring, tok, 1001, "transaction_commit", "tx-test", now) != nil, "UID mismatch rejected")
			add("capability_resource_bound", capability.Verify(ring, tok, 1000, "transaction_commit", "tx-other", now) != nil, "resource mismatch rejected")
			add("capability_expiry", capability.Verify(ring, tok, 1000, "transaction_commit", "tx-test", now.Add(2*time.Minute)) != nil, "expired capability rejected")
			repo, openErr := ledger.OpenRepository(filepath.Join(workDir, "conformance-ledger.db"))
			if openErr != nil {
				add("capability_single_use", false, openErr.Error())
			} else {
				d := capability.Digest(tok)
				first := repo.ConsumeAuthorizationCapability(tok.CapabilityID, "uid:1000", tok.OperationID, tok.ResourceID, d)
				second := repo.ConsumeAuthorizationCapability(tok.CapabilityID, "uid:1000", tok.OperationID, tok.ResourceID, d)
				_ = repo.Close()
				add("capability_single_use", first == nil && second != nil, "second capability consumption rejected")
			}
		}
	}
	r.Conformant = r.Failed == 0
	return r
}
func message(err error, success string) string {
	if err != nil {
		return err.Error()
	}
	return success
}
