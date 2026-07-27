package ratelimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const Version = "0.1"

type Policy struct {
	Version                   string `json:"version"`
	ReadRequestsPerMinute     int    `json:"read_requests_per_minute"`
	ReadBurst                 int    `json:"read_burst"`
	MutationRequestsPerMinute int    `json:"mutation_requests_per_minute"`
	MutationBurst             int    `json:"mutation_burst"`
	MaxConcurrentMutations    int    `json:"max_concurrent_mutations"`
}

func Default() Policy {
	return Policy{Version: Version, ReadRequestsPerMinute: 600, ReadBurst: 100, MutationRequestsPerMinute: 120, MutationBurst: 30, MaxConcurrentMutations: 8}
}

func (p Policy) Validate() error {
	if p.Version != Version {
		return fmt.Errorf("unsupported rate policy version %q", p.Version)
	}
	values := map[string]int{"read_requests_per_minute": p.ReadRequestsPerMinute, "read_burst": p.ReadBurst, "mutation_requests_per_minute": p.MutationRequestsPerMinute, "mutation_burst": p.MutationBurst, "max_concurrent_mutations": p.MaxConcurrentMutations}
	for name, value := range values {
		if value <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}
	if p.ReadBurst > p.ReadRequestsPerMinute*10 || p.MutationBurst > p.MutationRequestsPerMinute*10 {
		return errors.New("rate bursts are unreasonably larger than per-minute limits")
	}
	return nil
}

func Load(path string) (Policy, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Policy{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Policy{}, fmt.Errorf("rate policy must not be group/world writable: mode %o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return p, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return p, err
	}
	return p, p.Validate()
}

type bucket struct {
	Tokens  float64
	Updated time.Time
}

type Status struct {
	Version                   string `json:"version"`
	ReadRequestsPerMinute     int    `json:"read_requests_per_minute"`
	ReadBurst                 int    `json:"read_burst"`
	MutationRequestsPerMinute int    `json:"mutation_requests_per_minute"`
	MutationBurst             int    `json:"mutation_burst"`
	MaxConcurrentMutations    int    `json:"max_concurrent_mutations"`
	TrackedPrincipals         int    `json:"tracked_principals"`
	ActiveMutations           int    `json:"active_mutations"`
}

type Limiter struct {
	mu      sync.Mutex
	policy  Policy
	buckets map[string]bucket
	active  map[string]int
}

func New(policy Policy) (*Limiter, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Limiter{policy: policy, buckets: map[string]bucket{}, active: map[string]int{}}, nil
}

func (l *Limiter) Policy() Policy { l.mu.Lock(); defer l.mu.Unlock(); return l.policy }

func (l *Limiter) Status() Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	principals := map[string]struct{}{}
	active := 0
	for key := range l.buckets {
		if i := strings.IndexByte(key, 0); i >= 0 {
			principals[key[:i]] = struct{}{}
		}
	}
	for p, n := range l.active {
		principals[p] = struct{}{}
		active += n
	}
	p := l.policy
	return Status{Version: p.Version, ReadRequestsPerMinute: p.ReadRequestsPerMinute, ReadBurst: p.ReadBurst, MutationRequestsPerMinute: p.MutationRequestsPerMinute, MutationBurst: p.MutationBurst, MaxConcurrentMutations: p.MaxConcurrentMutations, TrackedPrincipals: len(principals), ActiveMutations: active}
}

// Begin consumes one request token. For mutations it also reserves one
// concurrency slot until the returned release function is called.
func (l *Limiter) Begin(principal string, mutation bool, now time.Time) (func(), time.Duration, error) {
	if l == nil {
		return func() {}, 0, nil
	}
	principal = strings.TrimSpace(principal)
	if principal == "" {
		principal = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.policy
	rpm, burst, kind := p.ReadRequestsPerMinute, p.ReadBurst, "read"
	if mutation {
		rpm, burst, kind = p.MutationRequestsPerMinute, p.MutationBurst, "mutation"
		if l.active[principal] >= p.MaxConcurrentMutations {
			return nil, time.Second, errors.New("principal mutation concurrency limit reached")
		}
	}
	key := principal + "\x00" + kind
	b, ok := l.buckets[key]
	if !ok {
		b = bucket{Tokens: float64(burst), Updated: now}
	}
	if now.Before(b.Updated) {
		now = b.Updated
	}
	elapsed := now.Sub(b.Updated).Seconds()
	b.Tokens += elapsed * float64(rpm) / 60.0
	if b.Tokens > float64(burst) {
		b.Tokens = float64(burst)
	}
	b.Updated = now
	if b.Tokens < 1 {
		l.buckets[key] = b
		wait := time.Duration((1 - b.Tokens) / (float64(rpm) / 60.0) * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		return nil, wait, errors.New("principal request rate exceeded")
	}
	b.Tokens--
	l.buckets[key] = b
	if mutation {
		l.active[principal]++
	}
	released := false
	return func() {
		if !mutation {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true
		if l.active[principal] <= 1 {
			delete(l.active, principal)
		} else {
			l.active[principal]--
		}
	}, 0, nil
}
