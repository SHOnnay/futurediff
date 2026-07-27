package peerauth

import (
	"context"
	"testing"
)

func TestContextIdentity(t *testing.T) {
	ctx := WithIdentity(context.Background(), Identity{UID: 1001, GID: 1002, PID: 42, Available: true})
	id, ok := FromContext(ctx)
	if !ok || id.UID != 1001 || Principal(ctx) != "uid:1001" {
		t.Fatalf("unexpected identity: %#v %t %s", id, ok, Principal(ctx))
	}
}
