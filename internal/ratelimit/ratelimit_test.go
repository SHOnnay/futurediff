package ratelimit

import (
	"testing"
	"time"
)

func TestRateAndConcurrency(t *testing.T) {
	p := Policy{Version: Version, ReadRequestsPerMinute: 60, ReadBurst: 1, MutationRequestsPerMinute: 60, MutationBurst: 2, MaxConcurrentMutations: 1}
	l, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	release, _, err := l.Begin("uid:1", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Begin("uid:1", true, now); err == nil {
		t.Fatal("expected concurrency rejection")
	}
	release()
	release2, _, err := l.Begin("uid:1", true, now)
	if err != nil {
		t.Fatal(err)
	}
	release2()
	if _, _, err := l.Begin("uid:1", true, now); err == nil {
		t.Fatal("expected token rejection")
	}
	if _, _, err := l.Begin("uid:1", false, now); err != nil {
		t.Fatal(err)
	}
	if _, wait, err := l.Begin("uid:1", false, now); err == nil || wait <= 0 {
		t.Fatal("expected read rate rejection")
	}
	if _, _, err := l.Begin("uid:1", false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}
