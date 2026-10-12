// Package browser signs the user in to NHS login in a real Chrome window. The
// user types their own details; the browser deals with NHS login's bot
// protection; and the redirect back to the NHS App is caught before it loads,
// so the authorization code is ours to exchange.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// ErrWindowClosed means the user closed the login window before finishing.
var ErrWindowClosed = errors.New("the login window was closed before signing in finished")

type Config struct {
	// Bin is the Chrome binary. When empty, an installed Chrome is used, or
	// Chromium is downloaded.
	Bin string
	// ProfileDir keeps the browser profile between logins, so NHS login can
	// remember the device and skip the security code.
	ProfileDir string
	// Headless hides the window, for tests.
	Headless bool
}

// Authorizer is an oauth.Authorizer that drives a browser. The browser stays
// open between calls, so a silent re-authorization can reuse NHS login's
// cookies, until Close.
type Authorizer struct {
	cfg Config

	mu       sync.Mutex
	launcher *launcher.Launcher
	browser  *rod.Browser
}

func New(cfg Config) *Authorizer {
	return &Authorizer{cfg: cfg}
}

func (a *Authorizer) Authorize(ctx context.Context, authorizeURL, redirectURI string) (*url.URL, error) {
	b, err := a.open()
	if err != nil {
		return nil, err
	}

	got := make(chan *url.URL, 1)
	router := b.HijackRequests()
	err = router.Add(redirectURI+"*", "", func(h *rod.Hijack) {
		select {
		case got <- h.Request.URL():
		default:
		}
		// Never load the NHS App: it would spend the code itself.
		h.Response.Fail(proto.NetworkErrorReasonAborted)
	})
	if err != nil {
		return nil, fmt.Errorf("browser: intercept redirect: %w", err)
	}
	go router.Run()
	defer func() { _ = router.Stop() }()

	page, err := b.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, fmt.Errorf("browser: open window: %w", err)
	}
	defer func() { _ = page.Close() }()
	if err := page.Context(ctx).Navigate(authorizeURL); err != nil {
		select {
		case u := <-got:
			// NHS login sent the user straight back, and we aborted that.
			return u, nil
		default:
			return nil, fmt.Errorf("browser: open login page: %w", err)
		}
	}

	evCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	closed := make(chan struct{})
	wait := b.Context(evCtx).EachEvent(func(e *proto.TargetTargetDestroyed) bool {
		if e.TargetID != page.TargetID {
			return false
		}
		close(closed)
		return true
	})
	go wait()

	select {
	case u := <-got:
		return u, nil
	case <-closed:
		return nil, ErrWindowClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts the browser, keeping its profile.
func (a *Authorizer) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.browser == nil {
		return nil
	}
	err := a.browser.Close()
	a.launcher.Kill()
	a.browser, a.launcher = nil, nil
	return err
}

func (a *Authorizer) open() (*rod.Browser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.browser != nil {
		return a.browser, nil
	}

	l := launcher.New().
		Leakless(false).
		Headless(a.cfg.Headless)
	if a.cfg.ProfileDir != "" {
		l = l.UserDataDir(a.cfg.ProfileDir)
	}
	if a.cfg.Bin != "" {
		l = l.Bin(a.cfg.Bin)
	}
	controlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("browser: launch: %w", err)
	}

	b := rod.New().ControlURL(controlURL).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		l.Kill()
		return nil, fmt.Errorf("browser: connect: %w", err)
	}
	a.launcher, a.browser = l, b
	return b, nil
}
