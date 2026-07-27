package effectspec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

type testAdapter struct{ states map[string]EffectStatus }

func (a *testAdapter) Describe(context.Context, string) (Descriptor, error) {
	return Descriptor{EffectSpec: Version, Adapter: "test", Tool: "mutate", Capabilities: Capabilities{Prepare: true, Preview: true, Verify: true, Commit: true, Abort: true, Compensate: true, Status: true}, MutatesState: true, Reversibility: Compensatable, PreviewFidelity: ExactPayload}, nil
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (a *testAdapter) Prepare(_ context.Context, c Context, input []byte) (PreparedEffect, error) {
	if a.states == nil {
		a.states = map[string]EffectStatus{}
	}
	a.states[c.EffectID] = StatusPrepared
	return PreparedEffect{Handle: c.EffectID, InputDigest: digest(input)}, nil
}
func (a *testAdapter) Preview(context.Context, Context, PreparedEffect) (Preview, error) {
	return Preview{Digest: digest([]byte("preview")), Summary: map[string]any{"ok": true}, Fidelity: ExactPayload}, nil
}
func (a *testAdapter) Verify(context.Context, Context, PreparedEffect) (Verification, error) {
	return Verification{Passed: true, EvidenceDigest: digest([]byte("evidence"))}, nil
}
func (a *testAdapter) Commit(_ context.Context, c Context, _ PreparedEffect) (Receipt, error) {
	a.states[c.EffectID] = StatusCommitted
	return Receipt{ProviderOperationID: "op", RequestDigest: digest([]byte("request")), CommittedAt: time.Now().UTC()}, nil
}
func (a *testAdapter) Abort(_ context.Context, c Context, _ PreparedEffect) error {
	a.states[c.EffectID] = StatusAborted
	return nil
}
func (a *testAdapter) Compensate(_ context.Context, c Context, _ Receipt) (Receipt, error) {
	a.states[c.EffectID] = StatusCompensated
	return Receipt{ProviderOperationID: "comp", RequestDigest: digest([]byte("comp")), CommittedAt: time.Now().UTC()}, nil
}
func (a *testAdapter) Status(_ context.Context, c Context, _ PreparedEffect) (StatusResult, error) {
	st := a.states[c.EffectID]
	r := StatusResult{Status: st}
	if st == StatusCommitted {
		rec := Receipt{RequestDigest: digest([]byte("request")), CommittedAt: time.Now().UTC()}
		r.Receipt = &rec
	}
	return r, nil
}

func TestRunConformance(t *testing.T) {
	r, err := RunConformance(context.Background(), &testAdapter{}, ConformanceOptions{Tool: "mutate", Input: []byte(`{"x":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed {
		t.Fatalf("report failed: %+v", r)
	}
}
func TestStrictDescriptor(t *testing.T) {
	_, err := ValidateDescriptorJSON([]byte(`{"effectspec":"0.1","adapter":"a","tool":"t","capabilities":{"prepare":true,"preview":false,"verify":false,"commit":true,"abort":false,"compensate":false,"status":false},"mutates_state":true,"open_world":false,"reversibility":"reversible","preview_fidelity":"unavailable","extra":1}`))
	if err == nil {
		t.Fatal("unknown field accepted")
	}
}
