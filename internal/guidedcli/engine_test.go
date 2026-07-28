package guidedcli

import (
	"reflect"
	"testing"
)

func TestRedactCommandArgsHidesApprovalDigestAndSecrets(t *testing.T) {
	got := redactCommandArgs([]string{"--socket", "/tmp/fd.sock", "approve", "tx_1", "digest-value", "--api-key", "secret-value"})
	want := []string{"--socket", "/tmp/fd.sock", "approve", "tx_1", "<transaction-digest>", "--api-key", "<redacted>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("redaction = %v, want %v", got, want)
	}
}
