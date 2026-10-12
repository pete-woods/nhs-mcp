// Package session logs in to the NHS App and holds the one user's session:
// NHS login, then an NHS App session, then a GP session for the record.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
)

var (
	loginScopes = []string{"openid", "profile", "email", "profile_extended", "gp_registration_details"}
	gpScopes    = []string{"openid", "profile", "email", "nhs_app_credentials", "gp_registration_details", "profile_extended"}
)

// The state the NHS App sends; it's only used to pick a page after login.
const state = "/patient"

// Defaults for Config.
const (
	DefaultLoginTimeout = 5 * time.Minute
	DefaultRecordTTL    = 15 * time.Minute
)

type Config struct {
	App *nhsapp.Client
	// Authorizer takes the user through NHS login. If it's an io.Closer it's
	// closed after each login.
	Authorizer oauth.Authorizer
	// AuthURL is NHS login, e.g. https://auth.login.nhs.uk.
	AuthURL string
	// LoginTimeout bounds how long the user has to log in.
	LoginTimeout time.Duration
	// RecordTTL is how long a fetched record is reused.
	RecordTTL time.Duration
}

// Manager is safe for concurrent use.
type Manager struct {
	cfg Config

	mu      sync.Mutex
	session *nhsapp.Session
	record  *nhsapp.Record
	fetched time.Time
}

func New(cfg Config) *Manager {
	if cfg.LoginTimeout == 0 {
		cfg.LoginTimeout = DefaultLoginTimeout
	}
	if cfg.RecordTTL == 0 {
		cfg.RecordTTL = DefaultRecordTTL
	}
	return &Manager{cfg: cfg}
}

// Status describes the session.
type Status struct {
	LoggedIn   bool
	GPSession  bool
	Name       string
	ODSCode    string
	ProofLevel string
}

// Login takes the user through NHS login and starts a GP session.
func (m *Manager) Login(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, m.cfg.LoginTimeout)
	defer cancel()
	if c, ok := m.cfg.Authorizer.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}

	m.forget()
	m.cfg.App.Reset()
	pkce := oauth.NewPKCE()
	redirectURI := m.cfg.App.LoginRedirectURI()
	code, err := m.authorize(ctx, oauth.AuthorizeRequest{
		ClientID:    nhsapp.ClientID,
		RedirectURI: redirectURI,
		Scopes:      loginScopes,
		State:       state,
		PKCE:        pkce,
	})
	if err != nil {
		return Status{}, err
	}
	s, err := m.cfg.App.CreateSession(ctx, nhsapp.AuthCode{Code: code, Verifier: pkce.Verifier, RedirectURI: redirectURI})
	if err != nil {
		return Status{}, fmt.Errorf("start NHS App session: %w", err)
	}
	m.session = &s

	// Start the GP session now, while NHS login still recognises the browser.
	if err := m.startGPSession(ctx); err != nil {
		slog.WarnContext(ctx, "Couldn't start a GP session; will retry when the record is needed", "error", err)
	}
	return m.status(), nil
}

// Status checks the session with the NHS App.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return Status{}, nil
	}
	s, err := m.cfg.App.Session(ctx)
	if errors.Is(err, nhsapp.ErrNotLoggedIn) {
		m.forget()
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	m.session = &s
	return m.status(), nil
}

// Logout ends the session.
func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil
	}
	m.forget()
	err := m.cfg.App.DeleteSession(ctx)
	if errors.Is(err, nhsapp.ErrNotLoggedIn) {
		return nil // it had already expired
	}
	return err
}

// Record returns the GP record, fetching it if the cached copy is stale.
func (m *Manager) Record(ctx context.Context) (nhsapp.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record != nil && time.Since(m.fetched) < m.cfg.RecordTTL {
		return *m.record, nil
	}
	r, err := withGPSession(ctx, m, m.cfg.App.MyRecord)
	if err != nil {
		return nhsapp.Record{}, err
	}
	m.record, m.fetched = &r, time.Now()
	return r, nil
}

// Demographics returns the patient's details as their GP has them.
func (m *Manager) Demographics(ctx context.Context) (nhsapp.Demographics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return withGPSession(ctx, m, m.cfg.App.Demographics)
}

// withGPSession calls fn, starting a GP session and trying again if fn needs
// one. The caller must hold m.mu.
func withGPSession[T any](ctx context.Context, m *Manager, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if m.session == nil {
		return zero, nhsapp.ErrNotLoggedIn
	}
	v, err := fn(ctx)
	if errors.Is(err, nhsapp.ErrNoGPSession) {
		if err := m.startGPSession(ctx); err != nil {
			return zero, err
		}
		v, err = fn(ctx)
	}
	if errors.Is(err, nhsapp.ErrNotLoggedIn) {
		m.forget()
	}
	return v, err
}

// startGPSession signs in to the GP system through NHS login, vouched for by
// the NHS App session so the user isn't asked to log in again.
func (m *Manager) startGPSession(ctx context.Context) error {
	identity, err := m.cfg.App.AssertedLoginIdentity(ctx)
	if err != nil {
		return fmt.Errorf("start GP session: %w", err)
	}
	pkce := oauth.NewPKCE()
	redirectURI := m.cfg.App.GPRedirectURI()
	code, err := m.authorize(ctx, oauth.AuthorizeRequest{
		ClientID:              nhsapp.ClientID,
		RedirectURI:           redirectURI,
		Scopes:                gpScopes,
		State:                 state,
		PKCE:                  pkce,
		AssertedLoginIdentity: identity,
		Silent:                true,
	})
	if err != nil {
		return fmt.Errorf("start GP session: %w", err)
	}
	s, err := m.cfg.App.CreateGPSession(ctx, nhsapp.AuthCode{Code: code, Verifier: pkce.Verifier, RedirectURI: redirectURI})
	if err != nil {
		return fmt.Errorf("start GP session: %w", err)
	}
	m.session = &s
	return nil
}

func (m *Manager) authorize(ctx context.Context, req oauth.AuthorizeRequest) (string, error) {
	redirect, err := m.cfg.Authorizer.Authorize(ctx, req.URL(m.cfg.AuthURL), req.RedirectURI)
	if err != nil {
		return "", err
	}
	return oauth.Code(redirect)
}

func (m *Manager) status() Status {
	if m.session == nil {
		return Status{}
	}
	return Status{
		LoggedIn:   true,
		GPSession:  m.session.HasGPSession,
		Name:       m.session.Name,
		ODSCode:    m.session.ODSCode,
		ProofLevel: m.session.ProofLevel,
	}
}

// forget drops the session and anything read with it.
func (m *Manager) forget() {
	m.session, m.record = nil, nil
}
