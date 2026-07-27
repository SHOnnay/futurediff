package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

type contextKey struct{}

var valid = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func Normalize(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !valid.MatchString(value) {
		return "", false
	}
	return value, true
}
func New() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req_unavailable"
	}
	return "req_" + hex.EncodeToString(b)
}
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}
func From(ctx context.Context) string { v, _ := ctx.Value(contextKey{}).(string); return v }
