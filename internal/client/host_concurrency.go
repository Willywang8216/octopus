package client

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
)

// OCTOPUS_HOST_CONCURRENCY_LIMITS limits in-flight upstream requests per host.
//
// Format:
//   host=limit,host2=limit2
// Examples:
//   free.9e.nz=5
//   free.9e.nz=5,api.openai.com=20
//
// Notes:
// - Matching is done against req.URL.Hostname() (no port).
// - Keys may include scheme/path; they will be normalized.
// - Keys starting with '.' are treated as suffix matches (e.g. ".9e.nz" matches "free.9e.nz").
const hostConcurrencyEnvKey = "OCTOPUS_HOST_CONCURRENCY_LIMITS"

type hostConcurrencyLimiter struct {
	mu sync.Mutex

	raw string

	// exact host -> limit
	exact map[string]int
	// suffixes (including leading '.') -> limit
	suffix map[string]int

	sems map[string]chan struct{} // host -> semaphore
}

func (l *hostConcurrencyLimiter) normalizeHostKey(key string) string {
	s := strings.TrimSpace(strings.ToLower(key))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if u, err := url.Parse(s); err == nil {
			if h := strings.ToLower(strings.TrimSpace(u.Hostname())); h != "" {
				return h
			}
		}
	}

	// Support inputs like "free.9e.nz/v1".
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}

	// Best-effort parse for host:port.
	if u, err := url.Parse("http://" + s); err == nil {
		if h := strings.ToLower(strings.TrimSpace(u.Hostname())); h != "" {
			return h
		}
	}
	return s
}

func (l *hostConcurrencyLimiter) reloadFromEnvLocked() {
	raw := strings.TrimSpace(os.Getenv(hostConcurrencyEnvKey))
	if raw == l.raw {
		return
	}

	l.raw = raw
	l.exact = make(map[string]int)
	l.suffix = make(map[string]int)
	l.sems = make(map[string]chan struct{})

	if raw == "" {
		return
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])

		limit, err := strconv.Atoi(v)
		if err != nil || limit <= 0 {
			continue
		}

		// Suffix match.
		if strings.HasPrefix(strings.TrimSpace(k), ".") {
			sfx := strings.ToLower(strings.TrimSpace(k))
			if sfx == "." {
				continue
			}
			l.suffix[sfx] = limit
			continue
		}

		host := l.normalizeHostKey(k)
		if host == "" {
			continue
		}
		l.exact[host] = limit
	}
}

func (l *hostConcurrencyLimiter) limitForHostLocked(host string) (int, bool) {
	if host == "" {
		return 0, false
	}
	if n, ok := l.exact[host]; ok {
		return n, true
	}
	for sfx, n := range l.suffix {
		needle := strings.TrimPrefix(sfx, ".")
		if needle == "" {
			continue
		}
		if host == needle || strings.HasSuffix(host, "."+needle) {
			return n, true
		}
	}
	return 0, false
}

func (l *hostConcurrencyLimiter) acquire(ctx context.Context, host string) (func(), error) {
	l.mu.Lock()
	l.reloadFromEnvLocked()
	limit, ok := l.limitForHostLocked(host)
	if !ok {
		l.mu.Unlock()
		return func() {}, nil
	}

	sem := l.sems[host]
	if sem == nil || cap(sem) != limit {
		sem = make(chan struct{}, limit)
		l.sems[host] = sem
	}
	l.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var defaultHostConcurrencyLimiter = &hostConcurrencyLimiter{}

type hostConcurrencyRoundTripper struct {
	base http.RoundTripper
}

func (t *hostConcurrencyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return t.base.RoundTrip(req)
	}

	host := strings.ToLower(strings.TrimSpace(req.URL.Hostname()))
	release, err := defaultHostConcurrencyLimiter.acquire(req.Context(), host)
	if err != nil {
		return nil, err
	}
	defer release()

	return t.base.RoundTrip(req)
}

func wrapWithHostConcurrencyLimit(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		return nil
	}
	return &hostConcurrencyRoundTripper{base: base}
}
