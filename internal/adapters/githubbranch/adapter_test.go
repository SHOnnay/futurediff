package githubbranch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedRunner gives tests independent control over the ls-remote and push
// boundaries so ambiguity, conflicts, and lease enforcement can be proven.
type scriptedRunner struct {
	oid           string
	lsRemoteErr   error
	pushErr       error
	pushSetsOID   bool
	pushSetsOIDTo string
	pushes        int
}

func (s *scriptedRunner) LSRemote(_ context.Context, _, _, _ string, _ []byte) (string, bool, error) {
	if s.lsRemoteErr != nil {
		return "", false, s.lsRemoteErr
	}
	if s.oid == "" {
		return "", false, nil
	}
	return s.oid, true, nil
}
func (s *scriptedRunner) PushCreateOnly(_ context.Context, _, _, _, _ string, _ []byte) error {
	s.pushes++
	if s.pushErr != nil {
		return s.pushErr
	}
	if s.pushSetsOID {
		s.oid = s.pushSetsOIDTo
	}
	return nil
}

type fakeRunner struct {
	oid       string
	pushes    int
	ambiguous bool
}

func (f *fakeRunner) LSRemote(_ context.Context, _, _, _ string, _ []byte) (string, bool, error) {
	if f.oid == "" {
		return "", false, nil
	}
	return f.oid, true, nil
}
func (f *fakeRunner) PushCreateOnly(_ context.Context, _, _, _, oid string, _ []byte) error {
	f.pushes++
	f.oid = oid
	if f.ambiguous {
		return errors.New("lost response")
	}
	return nil
}

func TestCreateOnlyPublicationAndStatus(t *testing.T) {
	oid := strings.Repeat("a", 40)
	tree := strings.Repeat("b", 40)
	runner := &fakeRunner{}
	a := &Adapter{Runner: runner}
	input := Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: oid, TreeOID: tree, Repository: t.TempDir()}
	prepared, _, err := a.Prepare(context.Background(), input, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := a.Publish(context.Background(), prepared, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CommitOID != oid || runner.pushes != 1 {
		t.Fatalf("receipt=%#v pushes=%d", receipt, runner.pushes)
	}
	status, err := a.Status(context.Background(), prepared, []byte("secret"))
	if err != nil || status.Status != StatusCommitted {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestPrepareRejectsExistingBranch(t *testing.T) {
	a := &Adapter{Runner: &fakeRunner{oid: strings.Repeat("c", 40)}}
	_, _, err := a.Prepare(context.Background(), Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Repository: t.TempDir()}, nil)
	if err == nil {
		t.Fatal("expected existing branch rejection")
	}
}

func TestStatusConflictIsClassified(t *testing.T) {
	a := &Adapter{Runner: &fakeRunner{oid: strings.Repeat("c", 40)}}
	status, err := a.Status(context.Background(), Prepared{Input: Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Repository: t.TempDir()}}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != StatusConflict || status.Receipt != nil || status.ObservedOID != strings.Repeat("c", 40) {
		t.Fatalf("status=%#v", status)
	}
}

func TestLSRemoteFailureIsAmbiguousStatus(t *testing.T) {
	a := &Adapter{Runner: &scriptedRunner{lsRemoteErr: errors.New("network partition")}}
	_, err := a.Status(context.Background(), Prepared{Input: Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Repository: t.TempDir()}}, []byte("secret"))
	var pe *ProviderError
	if !errors.As(err, &pe) || !pe.Ambiguous || pe.Class != "status_unknown" {
		t.Fatalf("err=%v", err)
	}
}

func TestPostPushMismatchIsAmbiguous(t *testing.T) {
	// The push command reports success but the remote branch resolves to a
	// foreign commit: the outcome is ambiguous and must not be replayed.
	runner := &scriptedRunner{pushSetsOID: true, pushSetsOIDTo: strings.Repeat("c", 40)}
	a := &Adapter{Runner: runner}
	prepared, _, err := a.Prepare(context.Background(), Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Repository: t.TempDir()}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Publish(context.Background(), prepared, []byte("secret"))
	var pe *ProviderError
	if !errors.As(err, &pe) || !pe.Ambiguous || pe.Class != "post_push_mismatch" {
		t.Fatalf("err=%v", err)
	}
	if runner.pushes != 1 {
		t.Fatalf("pushes=%d", runner.pushes)
	}
}

func TestPublishRequiresAbsentRefLease(t *testing.T) {
	a := &Adapter{Runner: &fakeRunner{}}
	_, err := a.Publish(context.Background(), Prepared{Input: Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40)}, ExpectedAbsent: false}, []byte("secret"))
	if err == nil || !strings.Contains(err.Error(), "absent-ref lease") {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyFreshReportsStaleWhenBranchAppears(t *testing.T) {
	a := &Adapter{Runner: &fakeRunner{oid: strings.Repeat("c", 40)}}
	err := a.VerifyFresh(context.Background(), Prepared{Input: Input{Owner: "acme", Repo: "app", Branch: "futurediff/tx1", RemoteURL: "https://github.com/acme/app.git", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Repository: t.TempDir()}}, []byte("secret"))
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Class != "stale_resource_version" {
		t.Fatalf("err=%v", err)
	}
}

func TestSanitizeRedactsToken(t *testing.T) {
	got := sanitize("remote: fatal: auth failed with token supersekrit123", []byte("supersekrit123"))
	if strings.Contains(got, "supersekrit123") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("sanitized=%q", got)
	}
}
