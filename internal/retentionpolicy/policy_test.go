package retentionpolicy

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	if err := Validate(Policy{Version: Version, TerminalAfterHours: 24, MaximumCandidates: 2}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(Policy{Version: Version, TerminalAfterHours: -1}); err == nil {
		t.Fatal("expected error")
	}
	_ = time.Hour
}
