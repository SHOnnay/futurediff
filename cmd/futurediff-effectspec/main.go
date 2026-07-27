package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SHOnnay/futurediff/effectspec"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
)

type referenceAdapter struct {
	states map[string]effectspec.EffectStatus
}

func (a *referenceAdapter) Describe(context.Context, string) (effectspec.Descriptor, error) {
	return effectspec.Descriptor{EffectSpec: effectspec.Version, Adapter: "reference", Tool: "reference.mutate", Capabilities: effectspec.Capabilities{Prepare: true, Preview: true, Verify: true, Commit: true, Abort: true, Compensate: true, Status: true}, MutatesState: true, Reversibility: effectspec.Compensatable, PreviewFidelity: effectspec.ExactPayload}, nil
}
func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (a *referenceAdapter) Prepare(_ context.Context, c effectspec.Context, input []byte) (effectspec.PreparedEffect, error) {
	if a.states == nil {
		a.states = map[string]effectspec.EffectStatus{}
	}
	a.states[c.EffectID] = effectspec.StatusPrepared
	return effectspec.PreparedEffect{Handle: c.EffectID, InputDigest: sum(input)}, nil
}
func (a *referenceAdapter) Preview(context.Context, effectspec.Context, effectspec.PreparedEffect) (effectspec.Preview, error) {
	return effectspec.Preview{Digest: sum([]byte("preview")), Summary: map[string]any{"reference": true}, Fidelity: effectspec.ExactPayload}, nil
}
func (a *referenceAdapter) Verify(context.Context, effectspec.Context, effectspec.PreparedEffect) (effectspec.Verification, error) {
	return effectspec.Verification{Passed: true, EvidenceDigest: sum([]byte("evidence"))}, nil
}
func (a *referenceAdapter) Commit(_ context.Context, c effectspec.Context, _ effectspec.PreparedEffect) (effectspec.Receipt, error) {
	a.states[c.EffectID] = effectspec.StatusCommitted
	return effectspec.Receipt{ProviderOperationID: "reference", RequestDigest: sum([]byte("commit")), CommittedAt: time.Now().UTC()}, nil
}
func (a *referenceAdapter) Abort(_ context.Context, c effectspec.Context, _ effectspec.PreparedEffect) error {
	a.states[c.EffectID] = effectspec.StatusAborted
	return nil
}
func (a *referenceAdapter) Compensate(_ context.Context, c effectspec.Context, _ effectspec.Receipt) (effectspec.Receipt, error) {
	a.states[c.EffectID] = effectspec.StatusCompensated
	return effectspec.Receipt{ProviderOperationID: "compensation", RequestDigest: sum([]byte("compensate")), CommittedAt: time.Now().UTC()}, nil
}
func (a *referenceAdapter) Status(_ context.Context, c effectspec.Context, _ effectspec.PreparedEffect) (effectspec.StatusResult, error) {
	st := a.states[c.EffectID]
	r := effectspec.StatusResult{Status: st}
	if st == effectspec.StatusCommitted {
		v := effectspec.Receipt{RequestDigest: sum([]byte("commit")), CommittedAt: time.Now().UTC()}
		r.Receipt = &v
	}
	return r, nil
}

func main() {
	descriptor := flag.String("descriptor", "", "validate an EffectSpec descriptor JSON")
	self := flag.Bool("self-test", false, "run the reference adapter conformance suite")
	version := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *version {
		fmt.Printf("%+v\n", buildinfo.Current())
		return
	}
	if *descriptor == "" && !*self {
		fail(fmt.Errorf("use --descriptor or --self-test"))
	}
	out := map[string]any{}
	ok := true
	if *descriptor != "" {
		b, e := os.ReadFile(*descriptor)
		if e != nil {
			fail(e)
		}
		d, e := effectspec.ValidateDescriptorJSON(b)
		if e != nil {
			fail(e)
		}
		out["descriptor"] = d
	}
	if *self {
		r, e := effectspec.RunConformance(context.Background(), &referenceAdapter{}, effectspec.ConformanceOptions{Tool: "reference.mutate", Input: []byte(`{"operation":"self_test"}`)})
		if e != nil {
			fail(e)
		}
		out["conformance"] = r
		ok = r.Passed
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	if !ok {
		os.Exit(2)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, "error:", e); os.Exit(1) }
