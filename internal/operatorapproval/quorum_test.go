package operatorapproval

import (
	"testing"
	"time"
)

func TestVerifyQuorum(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	a, ap, _ := Generate("alice", now)
	b, bp, _ := Generate("bob", now)
	ring := Keyring{Version: Version, Keys: []PublicKey{ap, bp}}
	e1, _ := Sign(a, "tx", "digest", time.Hour, now)
	e2, _ := Sign(b, "tx", "digest", time.Hour, now)
	bundle, err := NewBundle([]Envelope{e2, e1})
	if err != nil {
		t.Fatal(err)
	}
	policy := QuorumPolicy{Version: QuorumVersion, Threshold: 2, AllowedApprovers: []string{"alice", "bob"}, RequiredApprovers: []string{"alice"}}
	r, err := VerifyQuorum(ring, policy, bundle, "tx", "digest", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verified || len(r.DistinctApprovers) != 2 {
		t.Fatalf("bad result %+v", r)
	}
}
func TestQuorumRejectsDuplicateApproverAndMissing(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	a, ap, _ := Generate("alice", now)
	a2, ap2, _ := Generate("alice", now)
	ring := Keyring{Version: Version, Keys: []PublicKey{ap, ap2}}
	e1, _ := Sign(a, "tx", "d", time.Hour, now)
	e2, _ := Sign(a2, "tx", "d", time.Hour, now)
	b, _ := NewBundle([]Envelope{e1, e2})
	p := QuorumPolicy{Version: QuorumVersion, Threshold: 2}
	if _, err := VerifyQuorum(ring, p, b, "tx", "d", now); err == nil {
		t.Fatal("duplicate approver accepted")
	}
	one, _ := NewBundle([]Envelope{e1})
	if _, err := VerifyQuorum(ring, p, one, "tx", "d", now); err == nil {
		t.Fatal("missing quorum accepted")
	}
}
func TestBundleTamper(t *testing.T) {
	now := time.Now()
	a, _, _ := Generate("a", now)
	e, _ := Sign(a, "t", "d", time.Hour, now)
	b, _ := NewBundle([]Envelope{e})
	b.Envelopes[0].Approver = "x"
	if b.Digest == bundleDigest(b) {
		t.Fatal("tamper not visible")
	}
}
