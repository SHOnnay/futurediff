package githubbranch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
