package mcp_test

import (
	"net/http"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/api/mcp"
	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/session"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
	"github.com/pete-woods/nhs-mcp/internal/testing/testlog"
)

func TestLogin(t *testing.T) {
	fix := start(t)

	t.Run("tools need a login first", func(t *testing.T) {
		msg := mcptest.Error(t, fix.cs, "list_test_results", nil)
		assert.Check(t, cmp.Contains(msg, "call login"))
		assert.Check(t, cmp.DeepEqual(mcptest.Call[status](t, fix.cs, "get_login_status", nil), status{}))
	})

	t.Run("login starts an NHS App session and a GP session", func(t *testing.T) {
		got := mcptest.Call[status](t, fix.cs, "login", nil)
		assert.Check(t, cmp.DeepEqual(got, status{
			LoggedIn:   true,
			GPSession:  true,
			Name:       fakenhs.Name,
			ODSCode:    fakenhs.ODSCode,
			ProofLevel: fakenhs.ProofLevel,
		}))
		assert.Check(t, cmp.Equal(fix.nhs.Sessions(), 1))
	})

	t.Run("status asks the NHS App", func(t *testing.T) {
		got := mcptest.Call[status](t, fix.cs, "get_login_status", nil)
		assert.Check(t, got.LoggedIn)
		assert.Check(t, cmp.Equal(fix.nhs.Requests()[len(fix.nhs.Requests())-1].Path, "/v1/session"))
	})

	t.Run("logout ends the session", func(t *testing.T) {
		got := mcptest.Call[map[string]bool](t, fix.cs, "logout", nil)
		assert.Check(t, cmp.DeepEqual(got, map[string]bool{"logged_out": true}))
		assert.Check(t, cmp.Equal(fix.nhs.Sessions(), 0))
		assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "list_test_results", nil), "call login"))
	})

	t.Run("logging out twice is fine", func(t *testing.T) {
		mcptest.Call[map[string]bool](t, fix.cs, "logout", nil)
	})
}

func TestLogin_needsTheUser(t *testing.T) {
	fix := start(t)
	fix.nhs.SetInteractive(true)

	msg := mcptest.Error(t, fix.cs, "login", nil)
	assert.Check(t, cmp.Contains(msg, "NHS login needs the user to sign in"))
	assert.Check(t, cmp.Equal(fix.nhs.Sessions(), 0))
}

func TestSessionExpiry(t *testing.T) {
	fix := start(t)
	mcptest.Call[status](t, fix.cs, "login", nil)
	fix.nhs.ExpireSessions()

	assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "list_test_results", nil), "call login"))
	assert.Check(t, cmp.DeepEqual(mcptest.Call[status](t, fix.cs, "get_login_status", nil), status{}))

	t.Run("logging in again works", func(t *testing.T) {
		mcptest.Call[status](t, fix.cs, "login", nil)
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
		assert.Check(t, cmp.Len(got.Results, 6))
	})
}

func TestGPSessionRestarts(t *testing.T) {
	fix := start(t)
	mcptest.Call[status](t, fix.cs, "login", nil)
	fix.nhs.EndGPSessions()

	got := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
	assert.Check(t, cmp.Len(got.Results, 6))
	assert.Check(t, cmp.Contains(fix.nhs.Requests(), fakenhs.Request{Method: http.MethodGet, Path: "/v1/patient/my-record", Status: 598}))
}

func TestRecordIsCached(t *testing.T) {
	fix := start(t)
	mcptest.Call[status](t, fix.cs, "login", nil)

	mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
	mcptest.Call[map[string]any](t, fix.cs, "get_allergies", nil)
	mcptest.Call[map[string]any](t, fix.cs, "list_test_names", nil)

	fetches := 0
	for _, r := range fix.nhs.Requests() {
		if r.Path == "/v1/patient/my-record" {
			fetches++
		}
	}
	assert.Check(t, cmp.Equal(fetches, 1))
}

type fixture struct {
	nhs *fakenhs.Server
	cs  *sdk.ClientSession
}

// start serves the MCP against the fake NHS, not logged in.
func start(t *testing.T) fixture {
	testlog.Capture(t)
	nhs := fakenhs.New(t)
	sessions := session.New(session.Config{
		App:        nhsapp.New(nhsapp.Config{APIURL: nhs.AppURL(), WebURL: fakenhs.WebURL}),
		Authorizer: oauth.HTTPAuthorizer{},
		AuthURL:    nhs.LoginURL(),
	})
	cs := mcptest.Connect(t, mcp.New(mcp.Config{Sessions: sessions, Version: "test"}))
	return fixture{nhs: nhs, cs: cs}
}

// loggedIn is start, then login.
func loggedIn(t *testing.T) fixture {
	fix := start(t)
	mcptest.Call[status](t, fix.cs, "login", nil)
	return fix
}

type status struct {
	LoggedIn   bool   `json:"logged_in"`
	GPSession  bool   `json:"gp_session"`
	Name       string `json:"name"`
	ODSCode    string `json:"ods_code"`
	ProofLevel string `json:"proof_level"`
}
