package effectspec

import "testing"

func TestDescriptor(t *testing.T) {
	d := Descriptor{
		EffectSpec:      Version,
		Adapter:         "github",
		Tool:            "create_draft_pr",
		MutatesState:    true,
		Reversibility:   Compensatable,
		PreviewFidelity: ExactPayload,
		Capabilities: Capabilities{
			Commit: true, Prepare: true, Preview: true, Status: true, Compensate: true,
		},
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}
