package egress

import (
	"context"
	"net"
	"net/http"
	"testing"
)

type staticResolver map[string][]net.IPAddr

func (s staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return s[host], nil
}

func TestValidateRequestExactHostMethodAndPath(t *testing.T) {
	rule, err := RuleFromBase("https://api.github.com/repos", http.MethodGet, http.MethodPost)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := NewTransport(Policy{Rules: []Rule{rule}})
	if err != nil {
		t.Fatal(err)
	}
	good, _ := http.NewRequest(http.MethodPost, "https://api.github.com/repos/acme/app/pulls", nil)
	if err := tr.ValidateRequest(good); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"https://api.github.com.evil.test/repos/acme/app/pulls",
		"https://api.github.com/repositories/acme/app",
		"http://api.github.com/repos/acme/app",
		"https://127.0.0.1/repos/acme/app",
	}
	for _, raw := range cases {
		req, _ := http.NewRequest(http.MethodPost, raw, nil)
		if err := tr.ValidateRequest(req); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
}

func TestDNSAnswersRejectPrivateAndDocumentationRanges(t *testing.T) {
	rule, _ := RuleFromBase("https://provider.example/api", http.MethodGet)
	tr, _ := NewTransport(Policy{Rules: []Rule{rule}})
	tr.Resolver = staticResolver{"provider.example": {{IP: net.ParseIP("127.0.0.1")}}}
	if _, err := tr.dialContext(context.Background(), "tcp", "provider.example:443"); err == nil {
		t.Fatal("expected loopback rejection")
	}
	tr.Resolver = staticResolver{"provider.example": {{IP: net.ParseIP("203.0.113.5")}}}
	if _, err := tr.dialContext(context.Background(), "tcp", "provider.example:443"); err == nil {
		t.Fatal("expected documentation-range rejection")
	}
}

func TestRuleFromBaseRejectsUnsafeBase(t *testing.T) {
	for _, raw := range []string{"http://api.github.com", "https://127.0.0.1", "https://user@api.github.com", "https://api.github.com:444"} {
		if _, err := RuleFromBase(raw, http.MethodGet); err == nil {
			t.Fatalf("expected %s to fail", raw)
		}
	}
}
