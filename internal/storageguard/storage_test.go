package storageguard

import (
	"testing"
	"time"
)

type fakeProbe struct {
	fs              Filesystem
	ledger, runtime int64
}

func (f fakeProbe) Inspect(string, string, string) (Filesystem, int64, int64, error) {
	return f.fs, f.ledger, f.runtime, nil
}
func TestEvaluateFailsClosed(t *testing.T) {
	p := Policy{Version: Version, MinimumFreeBytes: 100, MaximumLedgerBytes: 50}
	s, e := Evaluate("/tmp", p, fakeProbe{fs: Filesystem{TotalBytes: 1000, FreeBytes: 10, FreePercent: 1}, ledger: 60}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if s.Healthy || len(s.Findings) != 2 {
		t.Fatalf("status=%+v", s)
	}
}
