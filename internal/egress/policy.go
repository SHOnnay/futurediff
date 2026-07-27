// Package egress provides a fail-closed HTTP transport for trusted provider adapters.
// It validates scheme, host, port, path, method, DNS answers, and redirect behavior
// before a credential-bearing request can leave the FutureDiff daemon.
package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Rule struct {
	Host         string   `json:"host"`
	Port         string   `json:"port,omitempty"`
	PathPrefixes []string `json:"path_prefixes"`
	Methods      []string `json:"methods"`
	AllowPrivate bool     `json:"allow_private,omitempty"`
}

type Policy struct {
	Rules []Rule `json:"rules"`
}

func RuleFromBase(raw string, methods ...string) (Rule, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Rule{}, err
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Rule{}, errors.New("provider base must be a credential-free HTTPS URL")
	}
	if u.Port() != "" && u.Port() != "443" {
		return Rule{}, errors.New("provider base must use the default HTTPS port")
	}
	if net.ParseIP(u.Hostname()) != nil {
		return Rule{}, errors.New("provider base must use a DNS hostname, not an IP literal")
	}
	prefix := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if prefix == "." {
		prefix = "/"
	}
	return Rule{Host: strings.ToLower(u.Hostname()), Port: "443", PathPrefixes: []string{prefix}, Methods: normalizeMethods(methods)}, nil
}

func (p Policy) Validate() error {
	if len(p.Rules) == 0 {
		return errors.New("at least one egress rule is required")
	}
	for i, r := range p.Rules {
		if strings.TrimSpace(r.Host) == "" || net.ParseIP(r.Host) != nil {
			return fmt.Errorf("rule %d requires a DNS hostname", i)
		}
		if r.Port == "" {
			r.Port = "443"
		}
		if r.Port != "443" {
			return fmt.Errorf("rule %d permits only HTTPS port 443", i)
		}
		if len(r.PathPrefixes) == 0 || len(r.Methods) == 0 {
			return fmt.Errorf("rule %d requires path prefixes and methods", i)
		}
		for _, prefix := range r.PathPrefixes {
			if !strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "..") {
				return fmt.Errorf("rule %d has unsafe path prefix %q", i, prefix)
			}
		}
	}
	return nil
}

type Transport struct {
	Policy   Policy
	Resolver Resolver
	Dialer   *net.Dialer
	Base     *http.Transport
}

func NewClient(policy Policy) (*http.Client, error) {
	transport, err := NewTransport(policy)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func NewTransport(policy Policy) (*Transport, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	t := &Transport{Policy: policy, Resolver: net.DefaultResolver, Dialer: dialer}
	t.Base = &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
	}
	t.Base.DialContext = t.dialContext
	return t, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ValidateRequest(req); err != nil {
		return nil, err
	}
	return t.Base.RoundTrip(req)
}

func (t *Transport) ValidateRequest(req *http.Request) error {
	if req == nil || req.URL == nil {
		return errors.New("request URL is required")
	}
	u := req.URL
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("egress request must use credential-free HTTPS")
	}
	if u.Port() != "" && u.Port() != "443" {
		return errors.New("egress request must use port 443")
	}
	if net.ParseIP(u.Hostname()) != nil {
		return errors.New("egress request cannot use an IP literal")
	}
	normalizedPath := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	for _, rule := range t.Policy.Rules {
		if !strings.EqualFold(rule.Host, u.Hostname()) {
			continue
		}
		if !containsFold(rule.Methods, req.Method) {
			continue
		}
		for _, prefix := range rule.PathPrefixes {
			cleanPrefix := path.Clean(prefix)
			if normalizedPath == cleanPrefix || strings.HasPrefix(normalizedPath, strings.TrimSuffix(cleanPrefix, "/")+"/") {
				return nil
			}
		}
	}
	return fmt.Errorf("request %s %s is outside the provider egress policy", req.Method, u.Redacted())
}

func (t *Transport) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	rule, ok := t.ruleForHost(host, port)
	if !ok {
		return nil, fmt.Errorf("dial destination %s is outside the provider egress policy", address)
	}
	resolver := t.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	answers, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve provider host: %w", err)
	}
	if len(answers) == 0 {
		return nil, errors.New("provider host resolved to no addresses")
	}
	ips := make([]netip.Addr, 0, len(answers))
	for _, answer := range answers {
		addr, ok := netip.AddrFromSlice(answer.IP)
		if !ok {
			return nil, errors.New("provider DNS answer is invalid")
		}
		addr = addr.Unmap()
		if !rule.AllowPrivate && forbidden(addr) {
			return nil, fmt.Errorf("provider DNS answer %s is not globally routable", addr)
		}
		ips = append(ips, addr)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].String() < ips[j].String() })
	dialer := t.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	var last error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, fmt.Errorf("connect to provider: %w", last)
}

func (t *Transport) ruleForHost(host, port string) (Rule, bool) {
	for _, rule := range t.Policy.Rules {
		rp := rule.Port
		if rp == "" {
			rp = "443"
		}
		if strings.EqualFold(rule.Host, host) && rp == port {
			return rule, true
		}
	}
	return Rule{}, false
}

func forbidden(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsPrivate() {
		return true
	}
	// Carrier-grade NAT, benchmark, documentation, and other non-public ranges.
	blocked := []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"}
	for _, raw := range blocked {
		prefix := netip.MustParsePrefix(raw)
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func normalizeMethods(methods []string) []string {
	if len(methods) == 0 {
		methods = []string{http.MethodGet, http.MethodPost}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method != "" && !seen[method] {
			seen[method] = true
			out = append(out, method)
		}
	}
	sort.Strings(out)
	return out
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
