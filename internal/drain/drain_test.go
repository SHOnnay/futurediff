package drain

import (
	"context"
	"testing"
	"time"
)

func TestDrainWaitsAndRejectsNew(t *testing.T) {
	m := New()
	release, err := m.BeginMutation()
	if err != nil {
		t.Fatal(err)
	}
	m.Start("shutdown", time.Now())
	if _, err := m.BeginMutation(); err == nil {
		t.Fatal("new mutation accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Wait(ctx); err == nil {
		t.Fatal("wait completed while active")
	}
	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := m.Wait(ctx2); err != nil {
		t.Fatal(err)
	}
	if m.Status().Active != 0 {
		t.Fatal("active not zero")
	}
}
func TestReleaseIdempotent(t *testing.T) {
	m := New()
	r, _ := m.BeginMutation()
	r()
	r()
	if m.Status().Active != 0 {
		t.Fatal("double release")
	}
}
