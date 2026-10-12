// Package nhsapp is a client for the NHS App's private web API
// (api.nhsapp.service.nhs.uk). Sessions are cookies plus a CSRF token, and
// GP record calls need a second, GP session on top.
package nhsapp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"github.com/pete-woods/nhs-mcp/internal/httpcl"
)

const (
	// ClientID is the NHS App's OAuth client at NHS login.
	ClientID = "nhs-online"

	// statusNoGPSession is the NHS App's status for "no GP session yet".
	statusNoGPSession = 598

	// webVersionTag is the web app version the API sees. Copied from the
	// web app the API was explored with.
	webVersionTag = "4.80.2 (commit:1ba12b3fa2)"
)

var (
	// ErrNotLoggedIn means there's no session, or it has expired.
	ErrNotLoggedIn = errors.New("not logged in to the NHS App")
	// ErrNoGPSession means the call needs a GP session first.
	ErrNoGPSession = errors.New("no GP session")
)

type Config struct {
	// APIURL is e.g. https://api.nhsapp.service.nhs.uk.
	APIURL string
	// WebURL is the web app, e.g. https://www.nhsapp.service.nhs.uk. The API
	// checks requests come from it, and it hosts the login redirect pages.
	WebURL string
	// Transport overrides the HTTP transport (useful for testing).
	Transport http.RoundTripper
}

// Client calls the API as one user. It's safe for concurrent use.
type Client struct {
	cfg Config

	mu        sync.Mutex
	cl        *httpcl.Client
	csrf      string
	patientID string
}

func New(cfg Config) *Client {
	c := &Client{cfg: cfg}
	c.Reset()
	return c
}

// Reset forgets the session.
func (c *Client) Reset() {
	jar, _ := cookiejar.New(nil) // never errors without options
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cl = httpcl.New(httpcl.Config{
		BaseURL:   c.cfg.APIURL,
		Transport: c.cfg.Transport,
		Jar:       jar,
		// The API answers 598 for "no GP session", which a retry won't fix,
		// and retrying a code exchange would spend the code twice.
		DisableRetries: true,
	})
	c.csrf, c.patientID = "", ""
}

// LoginRedirectURI is where NHS login sends the user after logging in.
func (c *Client) LoginRedirectURI() string {
	return c.cfg.WebURL + "/auth-return"
}

// GPRedirectURI is where NHS login sends the user when starting a GP session.
func (c *Client) GPRedirectURI() string {
	return c.cfg.WebURL + "/on-demand-gp-return"
}

// Session is the logged-in user.
type Session struct {
	Name             string `json:"name"`
	ODSCode          string `json:"odsCode"`
	ProofLevel       string `json:"proofLevel"`
	HasGPSession     bool   `json:"hasGpSession"`
	SessionTimeout   int    `json:"sessionTimeout"`
	Token            string `json:"token"`
	PatientSessionID string `json:"patientSessionId"`
}

// AuthCode is an authorization code from NHS login with its PKCE verifier.
type AuthCode struct {
	Code        string
	Verifier    string
	RedirectURI string
}

// CreateSession logs in with a code from NHS login's redirect to
// LoginRedirectURI.
func (c *Client) CreateSession(ctx context.Context, code AuthCode) (Session, error) {
	return c.exchange(ctx, http.MethodPost, "/v1/session", code)
}

// CreateGPSession starts a GP session with a code from NHS login's redirect
// to GPRedirectURI.
func (c *Client) CreateGPSession(ctx context.Context, code AuthCode) (Session, error) {
	return c.exchange(ctx, http.MethodPut, "/v1/session/gp-session-on-demand", code)
}

// Session returns the current session.
func (c *Client) Session(ctx context.Context) (Session, error) {
	var s Session
	err := c.call(ctx, httpcl.NewRequest(http.MethodGet, "/v1/session", httpcl.JSONDecoder(&s)))
	return s, err
}

// ExtendSession resets the session's idle timeout.
func (c *Client) ExtendSession(ctx context.Context) error {
	return c.call(ctx, httpcl.NewRequest(http.MethodPost, "/v1/session/extend", httpcl.Body(struct{}{})))
}

// DeleteSession logs out.
func (c *Client) DeleteSession(ctx context.Context) error {
	err := c.call(ctx, httpcl.NewRequest(http.MethodDelete, "/v1/session"))
	c.Reset()
	return err
}

// AssertedLoginIdentity returns a short-lived token that lets NHS login
// authorize a GP session without asking the user to log in again.
func (c *Client) AssertedLoginIdentity(ctx context.Context) (string, error) {
	var resp struct {
		Token string `json:"token"`
	}
	err := c.call(ctx, httpcl.NewRequest(http.MethodPost, "/v1/patient/asserted-login-identity",
		httpcl.Body(map[string]string{"IntendedRelyingPartyUrl": hostOf(c.cfg.WebURL)}),
		httpcl.JSONDecoder(&resp),
	))
	return resp.Token, err
}

// Demographics is the patient's details as their GP has them.
type Demographics struct {
	PatientName string `json:"patientName"`
	DateOfBirth string `json:"dateOfBirth"`
	Sex         string `json:"sex"`
	Address     string `json:"address"`
	NHSNumber   string `json:"nhsNumber"`
}

func (c *Client) Demographics(ctx context.Context) (Demographics, error) {
	var resp struct {
		Response Demographics `json:"response"`
	}
	err := c.call(ctx, httpcl.NewRequest(http.MethodGet, "/v1/patient/demographics", httpcl.JSONDecoder(&resp)))
	return resp.Response, err
}

// MyRecord fetches the whole GP record. It's large (around a megabyte for a
// long record) and every fetch is audited, so cache it.
func (c *Client) MyRecord(ctx context.Context) (Record, error) {
	var resp struct {
		Response Record `json:"response"`
	}
	err := c.call(ctx, httpcl.NewRequest(http.MethodGet, "/v1/patient/my-record", httpcl.JSONDecoder(&resp)))
	return resp.Response, err
}

func (c *Client) exchange(ctx context.Context, method, route string, code AuthCode) (Session, error) {
	var s Session
	err := c.call(ctx, httpcl.NewRequest(method, route,
		httpcl.Body(map[string]any{
			"authCode":            code.Code,
			"codeVerifier":        code.Verifier,
			"redirectUrl":         code.RedirectURI,
			"integrationReferrer": nil,
			"referrerOrigin":      nil,
		}),
		httpcl.JSONDecoder(&s),
	))
	if err != nil {
		return Session{}, err
	}
	c.mu.Lock()
	c.csrf, c.patientID = s.Token, s.PatientSessionID
	c.mu.Unlock()
	return s, nil
}

func (c *Client) call(ctx context.Context, r httpcl.Request) error {
	c.mu.Lock()
	cl, csrf, patientID := c.cl, c.csrf, c.patientID
	c.mu.Unlock()

	for _, h := range [][2]string{
		{"Origin", c.cfg.WebURL},
		{"NHSO-Request-ID", requestID()},
		{"NHSO-Web-Version-Tag", webVersionTag},
		{"X-CSRF-TOKEN", csrf},
		{"NHSO-Patient-Id", patientID},
	} {
		if h[1] != "" {
			httpcl.Header(h[0], h[1])(&r)
		}
	}

	_, err := cl.Call(ctx, r)
	switch {
	case httpcl.HasStatusCode(err, http.StatusUnauthorized, http.StatusForbidden):
		return fmt.Errorf("%w: %w", ErrNotLoggedIn, err)
	case httpcl.HasStatusCode(err, statusNoGPSession):
		return fmt.Errorf("%w: %w", ErrNoGPSession, err)
	}
	return err
}

// requestID is a random UUID (v4), which the API wants on every request.
func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func hostOf(webURL string) string {
	u, err := url.Parse(webURL)
	if err != nil {
		return webURL
	}
	return u.Host
}
