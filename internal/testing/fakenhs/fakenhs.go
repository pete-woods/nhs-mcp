// Package fakenhs fakes NHS login and the NHS App API well enough to log in
// and read a record: authorization codes are checked against their PKCE
// challenge, sessions are cookies plus a CSRF token, and the GP record needs
// a GP session started through an asserted login identity, as for real.
package fakenhs

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// The fake's fixed identities.
const (
	ClientID = "nhs-online"
	// WebURL is the NHS App web app. Nothing listens there: logins must catch
	// the redirect to it rather than follow it.
	WebURL = "https://www.nhsapp.test"

	Name       = "Test Patient"
	ODSCode    = "A12345"
	ProofLevel = "P9"
	NHSNumber  = "999 000 0000"

	// statusNoGPSession is the NHS App's status for "no GP session yet".
	statusNoGPSession = 598
)

// Record is the default record: synthetic, not anyone's real data.
//
//go:embed testdata/record.json
var Record []byte

// Request is a request one of the fakes received.
type Request struct {
	Method, Path string
	Status       int
}

type grant struct {
	challenge   string
	redirectURI string
	gp          bool
}

type session struct {
	csrf      string
	patientID string
	gp        bool
}

type Server struct {
	login *httptest.Server
	app   *httptest.Server

	mu          sync.Mutex
	interactive bool
	record      []byte
	grants      map[string]grant
	identities  map[string]bool
	sessions    map[string]*session
	requests    []Request
}

func New(t testing.TB) *Server {
	s := &Server{
		record:     Record,
		grants:     map[string]grant{},
		identities: map[string]bool{},
		sessions:   map[string]*session{},
	}

	login := http.NewServeMux()
	login.HandleFunc("GET /authorize", s.handleAuthorize)
	login.HandleFunc("GET /authcode", s.handleAuthcode)
	s.login = httptest.NewServer(s.logRequests(login))
	t.Cleanup(s.login.Close)

	app := http.NewServeMux()
	app.HandleFunc("POST /v1/session", s.handleCreateSession)
	app.HandleFunc("GET /v1/session", s.authed(s.handleGetSession))
	app.HandleFunc("DELETE /v1/session", s.authed(s.handleDeleteSession))
	app.HandleFunc("POST /v1/session/extend", s.authed(func(http.ResponseWriter, *http.Request, *session) {}))
	app.HandleFunc("PUT /v1/session/gp-session-on-demand", s.authed(s.handleCreateGPSession))
	app.HandleFunc("POST /v1/patient/asserted-login-identity", s.authed(s.handleAssertedLoginIdentity))
	app.HandleFunc("GET /v1/patient/my-record", s.authed(s.gp(s.handleRecord)))
	app.HandleFunc("GET /v1/patient/demographics", s.authed(s.gp(s.handleDemographics)))
	s.app = httptest.NewServer(s.logRequests(app))
	t.Cleanup(s.app.Close)
	return s
}

// LoginURL is the fake NHS login, standing in for https://auth.login.nhs.uk.
func (s *Server) LoginURL() string { return s.login.URL }

// AppURL is the fake NHS App API, standing in for https://api.nhsapp.service.nhs.uk.
func (s *Server) AppURL() string { return s.app.URL }

// SetInteractive makes NHS login show its login page, as when the user hasn't
// signed in, instead of redirecting straight back with a code. Silent
// re-authorizations with an asserted login identity still redirect.
func (s *Server) SetInteractive(interactive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactive = interactive
}

// SetRecord replaces the record, a my-record response body.
func (s *Server) SetRecord(body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = body
}

// ExpireSessions ends every session, as the NHS App does after ten minutes idle.
func (s *Server) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.sessions)
}

// EndGPSessions drops the GP side of every session.
func (s *Server) EndGPSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		sess.gp = false
	}
}

// Sessions counts the live sessions.
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Requests lists the requests both fakes have had.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// handleAuthorize stands in for the user signing in. Like NHS login it
// redirects through more of its own pages before returning to the app.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Get("client_id") != ClientID,
		q.Get("response_type") != "code",
		q.Get("code_challenge_method") != "S256",
		q.Get("code_challenge") == "",
		!strings.HasPrefix(q.Get("redirect_uri"), WebURL+"/"):
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}

	g := grant{challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
	s.mu.Lock()
	if identity := q.Get("asserted_login_identity"); identity != "" {
		if !s.identities[identity] {
			s.mu.Unlock()
			redirectError(w, r, g.redirectURI, "access_denied")
			return
		}
		delete(s.identities, identity)
		g.gp = true
	} else if s.interactive {
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><form>Enter your email address</form></body></html>"))
		return
	}
	code := token()
	s.grants[code] = g
	s.mu.Unlock()

	next := url.URL{Path: "/authcode", RawQuery: url.Values{
		"code":         {code},
		"redirect_uri": {g.redirectURI},
		"state":        {q.Get("state")},
	}.Encode()}
	http.Redirect(w, r, next.String(), http.StatusFound)
}

func (s *Server) handleAuthcode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back := q.Get("redirect_uri") + "?" + url.Values{"code": {q.Get("code")}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, back, http.StatusFound)
}

type exchange struct {
	AuthCode     string `json:"authCode"`
	CodeVerifier string `json:"codeVerifier"`
	RedirectURL  string `json:"redirectUrl"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if !s.redeem(w, r, false) {
		return
	}
	id := token()
	sess := &session{csrf: token(), patientID: token()}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{Name: "NHSO-Session-Id", Value: id, Path: "/", HttpOnly: true})
	writeJSON(w, http.StatusCreated, s.sessionJSON(sess))
}

func (s *Server) handleCreateGPSession(w http.ResponseWriter, r *http.Request, sess *session) {
	if !s.redeem(w, r, true) {
		return
	}
	s.mu.Lock()
	sess.gp = true
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, s.sessionJSON(sess))
}

func (s *Server) handleGetSession(w http.ResponseWriter, _ *http.Request, sess *session) {
	writeJSON(w, http.StatusOK, s.sessionJSON(sess))
}

func (s *Server) handleDeleteSession(_ http.ResponseWriter, r *http.Request, _ *session) {
	c, _ := r.Cookie("NHSO-Session-Id")
	s.mu.Lock()
	delete(s.sessions, c.Value)
	s.mu.Unlock()
}

func (s *Server) handleAssertedLoginIdentity(w http.ResponseWriter, r *http.Request, _ *session) {
	var body struct {
		IntendedRelyingPartyURL string `json:"IntendedRelyingPartyUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IntendedRelyingPartyURL == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	identity := token()
	s.mu.Lock()
	s.identities[identity] = true
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"token": identity})
}

func (s *Server) handleRecord(w http.ResponseWriter, _ *http.Request, _ *session) {
	s.mu.Lock()
	body := s.record
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

func (s *Server) handleDemographics(w http.ResponseWriter, _ *http.Request, _ *session) {
	writeJSON(w, http.StatusOK, map[string]any{"response": map[string]any{
		"patientName": "Mx " + Name,
		"dateOfBirth": "1980-04-01T00:00:00",
		"sex":         "Unknown",
		"address":     "1 Test Street, Testtown, TE1 1ST",
		"nhsNumber":   NHSNumber,
	}})
}

// redeem spends the authorization code in the request body.
func (s *Server) redeem(w http.ResponseWriter, r *http.Request, gp bool) bool {
	var ex exchange
	if err := json.NewDecoder(r.Body).Decode(&ex); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	s.mu.Lock()
	g, ok := s.grants[ex.AuthCode]
	delete(s.grants, ex.AuthCode)
	s.mu.Unlock()

	sum := sha256.Sum256([]byte(ex.CodeVerifier))
	if !ok || g.gp != gp || g.redirectURI != ex.RedirectURL ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorCode": 1, "errorMessage": "invalid authorization code"})
		return false
	}
	return true
}

// authed requires the session cookie, the CSRF token and the patient id.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("NHSO-Session-Id")
		var sess *session
		if err == nil {
			s.mu.Lock()
			sess = s.sessions[c.Value]
			s.mu.Unlock()
		}
		switch {
		case sess == nil:
			w.WriteHeader(http.StatusUnauthorized)
		case r.Header.Get("X-CSRF-TOKEN") != sess.csrf,
			r.Header.Get("Origin") != WebURL:
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path != "/v1/session" && r.Header.Get("NHSO-Patient-Id") != sess.patientID:
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorCode": 2, "errorMessage": "unknown patient"})
		default:
			next(w, r, sess)
		}
	}
}

// gp requires a GP session, answering 598 as the NHS App does without one.
func (s *Server) gp(next func(http.ResponseWriter, *http.Request, *session)) func(http.ResponseWriter, *http.Request, *session) {
	return func(w http.ResponseWriter, r *http.Request, sess *session) {
		s.mu.Lock()
		gp := sess.gp
		s.mu.Unlock()
		if !gp {
			writeJSON(w, statusNoGPSession, map[string]any{"serviceDeskReference": "fake", "errorCode": 0, "errorMessage": nil})
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) sessionJSON(sess *session) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"name":             Name,
		"sessionTimeout":   600,
		"odsCode":          ODSCode,
		"token":            sess.csrf,
		"nhsNumber":        NHSNumber,
		"hasGpSession":     sess.gp,
		"patientSessionId": sess.patientID,
		"proofLevel":       ProofLevel,
	}
}

// logRequests notes each request and its status.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.mu.Lock()
		s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Status: rec.status})
		s.mu.Unlock()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func redirectError(w http.ResponseWriter, r *http.Request, redirectURI, code string) {
	http.Redirect(w, r, redirectURI+"?"+url.Values{"error": {code}}.Encode(), http.StatusFound)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func token() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
