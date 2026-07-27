package peerauth

import "context"

type Identity struct {
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	PID       int32  `json:"pid"`
	Available bool   `json:"available"`
}

type contextKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok && id.Available
}

func Principal(ctx context.Context) string {
	if id, ok := FromContext(ctx); ok {
		return "uid:" + uintString(id.UID)
	}
	return "local:unknown"
}

func uintString(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
