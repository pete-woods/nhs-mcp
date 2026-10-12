// Package egress catches requests that would leave the machine. Point a
// process's proxy at a Trap and anything not bound for loopback lands here
// and is refused, instead of reaching the real NHS.
package egress

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request is one request that tried to leave.
type Request struct {
	Method, Host string
}

type Trap struct {
	srv *httptest.Server

	mu       sync.Mutex
	requests []Request
}

// New starts a trap. The test fails at cleanup if a request escaped and the
// test didn't Take it.
func New(t testing.TB) *Trap {
	p := &Trap{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, Request{Method: r.Method, Host: r.Host})
		p.mu.Unlock()
		http.Error(w, "egress blocked by test", http.StatusForbidden)
	}))
	t.Cleanup(func() {
		p.srv.Close()
		for _, r := range p.Take() {
			t.Errorf("a request escaped the fakes: %s %s", r.Method, r.Host)
		}
	})
	return p
}

func (p *Trap) URL() string {
	return p.srv.URL
}

// Env sets the proxy, in every spelling, to the trap, leaving loopback direct.
func (p *Trap) Env() []string {
	return []string{
		"HTTP_PROXY=" + p.URL(), "http_proxy=" + p.URL(),
		"HTTPS_PROXY=" + p.URL(), "https_proxy=" + p.URL(),
		"NO_PROXY=127.0.0.1,localhost", "no_proxy=127.0.0.1,localhost",
	}
}

// Take returns and forgets the requests caught so far.
func (p *Trap) Take() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.requests
	p.requests = nil
	return r
}
