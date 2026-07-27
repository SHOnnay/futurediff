package operatorapproval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const QuorumVersion = "0.1"

type QuorumPolicy struct {
	Version           string   `json:"version"`
	Threshold         int      `json:"threshold"`
	AllowedApprovers  []string `json:"allowed_approvers,omitempty"`
	RequiredApprovers []string `json:"required_approvers,omitempty"`
}

type Bundle struct {
	Version   string     `json:"version"`
	Envelopes []Envelope `json:"envelopes"`
	Digest    string     `json:"digest"`
}

type QuorumResult struct {
	Verified          bool     `json:"verified"`
	Threshold         int      `json:"threshold"`
	DistinctApprovers []string `json:"distinct_approvers"`
	SignatureRefs     []string `json:"signature_refs"`
	BundleDigest      string   `json:"bundle_digest"`
}

func ValidateQuorumPolicy(p QuorumPolicy) error {
	if p.Version != QuorumVersion {
		return errors.New("unsupported approval quorum policy version")
	}
	if p.Threshold < 1 {
		return errors.New("approval quorum threshold must be at least one")
	}
	allowed, err := normalizedUnique(p.AllowedApprovers)
	if err != nil {
		return fmt.Errorf("allowed approvers: %w", err)
	}
	required, err := normalizedUnique(p.RequiredApprovers)
	if err != nil {
		return fmt.Errorf("required approvers: %w", err)
	}
	if len(allowed) > 0 && p.Threshold > len(allowed) {
		return errors.New("approval quorum threshold exceeds allowed approver count")
	}
	if len(required) > p.Threshold {
		return errors.New("required approver count exceeds threshold")
	}
	if len(allowed) > 0 {
		set := toSet(allowed)
		for _, a := range required {
			if !set[a] {
				return fmt.Errorf("required approver %q is not allowed", a)
			}
		}
	}
	return nil
}

func NewBundle(envelopes []Envelope) (Bundle, error) {
	if len(envelopes) == 0 {
		return Bundle{}, errors.New("at least one approval envelope is required")
	}
	copyEnvs := append([]Envelope(nil), envelopes...)
	sort.Slice(copyEnvs, func(i, j int) bool {
		if copyEnvs[i].Approver != copyEnvs[j].Approver {
			return copyEnvs[i].Approver < copyEnvs[j].Approver
		}
		if copyEnvs[i].KeyID != copyEnvs[j].KeyID {
			return copyEnvs[i].KeyID < copyEnvs[j].KeyID
		}
		return copyEnvs[i].Nonce < copyEnvs[j].Nonce
	})
	b := Bundle{Version: QuorumVersion, Envelopes: copyEnvs}
	b.Digest = bundleDigest(b)
	return b, nil
}

func VerifyQuorum(ring Keyring, policy QuorumPolicy, bundle Bundle, transactionID, digest string, now time.Time) (QuorumResult, error) {
	if err := ValidateQuorumPolicy(policy); err != nil {
		return QuorumResult{}, err
	}
	if bundle.Version != QuorumVersion || len(bundle.Envelopes) == 0 || bundle.Digest != bundleDigest(bundle) {
		return QuorumResult{}, errors.New("invalid approval quorum bundle")
	}
	allowed := toSet(policy.AllowedApprovers)
	required := toSet(policy.RequiredApprovers)
	approvers := map[string]bool{}
	keys := map[string]bool{}
	nonces := map[string]bool{}
	refs := make([]string, 0, len(bundle.Envelopes))
	for _, env := range bundle.Envelopes {
		if err := Verify(ring, env, transactionID, digest, now); err != nil {
			return QuorumResult{}, fmt.Errorf("approval from %s: %w", env.Approver, err)
		}
		if len(allowed) > 0 && !allowed[env.Approver] {
			return QuorumResult{}, fmt.Errorf("approver %q is not allowed", env.Approver)
		}
		if approvers[env.Approver] {
			return QuorumResult{}, fmt.Errorf("duplicate approver %q", env.Approver)
		}
		if keys[env.KeyID] {
			return QuorumResult{}, fmt.Errorf("duplicate approval key %q", env.KeyID)
		}
		if nonces[env.Nonce] {
			return QuorumResult{}, errors.New("duplicate approval nonce")
		}
		approvers[env.Approver], keys[env.KeyID], nonces[env.Nonce] = true, true, true
		refs = append(refs, SignatureReference(env))
	}
	if len(approvers) < policy.Threshold {
		return QuorumResult{}, fmt.Errorf("approval quorum not met: have=%d need=%d", len(approvers), policy.Threshold)
	}
	for a := range required {
		if !approvers[a] {
			return QuorumResult{}, fmt.Errorf("required approver %q is missing", a)
		}
	}
	names := make([]string, 0, len(approvers))
	for a := range approvers {
		names = append(names, a)
	}
	sort.Strings(names)
	sort.Strings(refs)
	return QuorumResult{Verified: true, Threshold: policy.Threshold, DistinctApprovers: names, SignatureRefs: refs, BundleDigest: bundle.Digest}, nil
}

func LoadQuorumPolicy(path string) (QuorumPolicy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return QuorumPolicy{}, err
	}
	var p QuorumPolicy
	if err := strictJSON(b, &p); err != nil {
		return p, err
	}
	return p, ValidateQuorumPolicy(p)
}
func WriteQuorumPolicy(path string, p QuorumPolicy) error {
	if err := ValidateQuorumPolicy(p); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), 0o600)
}
func LoadBundle(path string) (Bundle, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	var v Bundle
	if err := strictJSON(b, &v); err != nil {
		return v, err
	}
	if v.Version != QuorumVersion || v.Digest != bundleDigest(v) {
		return v, errors.New("invalid approval quorum bundle")
	}
	return v, nil
}
func WriteBundle(path string, b Bundle) error {
	if b.Version != QuorumVersion || b.Digest != bundleDigest(b) {
		return errors.New("invalid approval quorum bundle")
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func QuorumSignatureReference(result QuorumResult) string {
	return "ed25519-quorum:" + result.BundleDigest
}
func bundleDigest(b Bundle) string {
	b.Digest = ""
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func normalizedUnique(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, errors.New("empty approver")
		}
		if seen[v] {
			return nil, fmt.Errorf("duplicate approver %q", v)
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}
func toSet(values []string) map[string]bool {
	s := map[string]bool{}
	for _, v := range values {
		s[strings.TrimSpace(v)] = true
	}
	return s
}
