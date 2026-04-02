package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostConcurrencyLimitRoundTripper(t *testing.T) {
	var active int64
	var maxActive int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := atomic.AddInt64(&active, 1)
		for {
			prev := atomic.LoadInt64(&maxActive)
			if now <= prev {
				break
			}
			if atomic.CompareAndSwapInt64(&maxActive, prev, now) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		atomic.AddInt64(&active, -1)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host := u.Hostname()

	// Limit this host to 5 concurrent in-flight requests.
	t.Setenv(hostConcurrencyEnvKey, host+"=5")

	base, err := clonedDefaultTransport()
	if err != nil {
		t.Fatalf("clone transport: %v", err)
	}
	client := &http.Client{Transport: wrapWithHostConcurrencyLimit(base)}

	const total = 25
	wg := sync.WaitGroup{}
	wg.Add(total)

	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}

	wg.Wait()

	if max := atomic.LoadInt64(&maxActive); max > 5 {
		t.Fatalf("expected max concurrency <= 5, got %d", max)
	}
}
