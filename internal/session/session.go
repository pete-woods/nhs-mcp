// Package session logs in to the NHS App and holds the one user's session:
// NHS login, then an NHS App session, then a GP session for the record.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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
	// detailedResults is whether results need TestResultDetail.
	detailedResults bool
	record          *nhsapp.Record
	fetched         time.Time
	historic        map[int]nhsapp.TestResultsSection
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
	if r := s.ServiceJourneyRules; r != nil {
		v := r.Journeys.IM1TestResults.Version
		m.detailedResults = v != "" && v != "3"
	}

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
	m.record, m.fetched, m.historic = &r, time.Now(), map[int]nhsapp.TestResultsSection{}
	return r, nil
}

// maxParallel bounds concurrent requests to the NHS App.
const maxParallel = 4

// RecordFrom is Record, with older test results back to the start of
// fromYear fetched too when the record doesn't already include them. It
// also returns the years that couldn't be fetched.
func (m *Manager) RecordFrom(ctx context.Context, fromYear int) (nhsapp.Record, []int, error) {
	r, err := m.Record(ctx)
	if err != nil || r.TestResults.HasAllHistoricResults {
		return r, nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var (
		resMu   sync.Mutex
		fetched = map[int]nhsapp.TestResultsSection{}
		failed  []int
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)
	for year := fromYear; year <= time.Now().Year(); year++ {
		if _, ok := m.historic[year]; ok {
			continue
		}
		g.Go(func() error {
			h, err := m.cfg.App.HistoricTestResults(gctx, year)
			if errors.Is(err, nhsapp.ErrNotLoggedIn) {
				return err
			}
			resMu.Lock()
			defer resMu.Unlock()
			if err != nil {
				slog.WarnContext(ctx, "Couldn't fetch historic test results", "year", year, "error", err)
				failed = append(failed, year)
				return nil
			}
			fetched[year] = h
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		if errors.Is(err, nhsapp.ErrNotLoggedIn) {
			m.forget()
		}
		return nhsapp.Record{}, nil, err
	}
	maps.Copy(m.historic, fetched)
	slices.Sort(failed)
	have := slices.Concat(r.TestResults.Data, r.HistoricTestResults.Data)
	r.HistoricTestResults.Data = slices.Concat(r.HistoricTestResults.Data, m.historicSince(have))
	return r, failed, nil
}

// historicSince merges every cached historic year, leaving out results
// already in current, so results found once can still be looked up by id.
// The caller must hold m.mu.
func (m *Manager) historicSince(current []nhsapp.TestResult) []nhsapp.TestResult {
	seen := map[string]bool{}
	for _, tr := range current {
		seen[tr.ID] = true
	}
	var out []nhsapp.TestResult
	for _, year := range slices.Sorted(maps.Keys(m.historic)) {
		for _, tr := range m.historic[year].Data {
			if !seen[tr.ID] {
				seen[tr.ID] = true
				out = append(out, tr)
			}
		}
	}
	return out
}

// TestResultDetail returns a result's detail as HTML, for GP systems whose
// record doesn't break results into values. ok is false when the record
// already has everything.
func (m *Manager) TestResultDetail(ctx context.Context, id string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.detailedResults {
		return "", false, nil
	}
	html, err := withGPSession(ctx, m, func(ctx context.Context) (string, error) {
		return m.cfg.App.TestResultDetail(ctx, id)
	})
	return html, err == nil, err
}

var (
	// ErrNoDocument means there's no document with that id in the record.
	ErrNoDocument = errors.New("no document with that id")
	// ErrDocumentUnavailable means the GP system can't supply the document.
	ErrDocumentUnavailable = errors.New("the GP system can't supply this document")
)

// Document fetches a document in the record for viewing.
func (m *Manager) Document(ctx context.Context, id string) (nhsapp.Document, nhsapp.DocumentContent, error) {
	d, err := m.document(ctx, id)
	if err != nil {
		return d, nhsapp.DocumentContent{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := withGPSession(ctx, m, func(ctx context.Context) (nhsapp.DocumentContent, error) {
		return m.cfg.App.Document(ctx, d)
	})
	return d, c, err
}

// File is a document's original file.
type File struct {
	Data        []byte
	ContentType string
}

// DownloadDocument fetches the original file of a document in the record.
func (m *Manager) DownloadDocument(ctx context.Context, id string) (nhsapp.Document, File, error) {
	d, err := m.document(ctx, id)
	if err != nil {
		return d, File{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := withGPSession(ctx, m, func(ctx context.Context) (File, error) {
		data, contentType, err := m.cfg.App.DownloadDocument(ctx, d)
		return File{Data: data, ContentType: contentType}, err
	})
	return d, f, err
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
	m.session, m.record, m.historic, m.detailedResults = nil, nil, nil, false
}

// document finds a document in the record that the GP system can supply.
func (m *Manager) document(ctx context.Context, id string) (nhsapp.Document, error) {
	r, err := m.Record(ctx)
	if err != nil {
		return nhsapp.Document{}, err
	}
	i := slices.IndexFunc(r.Documents.Data, func(d nhsapp.Document) bool { return d.DocumentIdentifier == id })
	if i < 0 {
		return nhsapp.Document{}, ErrNoDocument
	}
	d := r.Documents.Data[i]
	if !d.IsAvailable || !d.IsValidFile {
		return d, ErrDocumentUnavailable
	}
	return d, nil
}
