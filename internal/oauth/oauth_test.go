package oauth_test

import (
	"net/url"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
)

func TestS256(t *testing.T) {
	// The example from RFC 7636, appendix B.
	assert.Check(t, cmp.Equal(oauth.S256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"), "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"))
}

func TestNewPKCE(t *testing.T) {
	a, b := oauth.NewPKCE(), oauth.NewPKCE()
	assert.Check(t, cmp.Len(a.Verifier, 43))
	assert.Check(t, cmp.Equal(a.Challenge, oauth.S256(a.Verifier)))
	assert.Check(t, a.Verifier != b.Verifier, "verifiers must be random")
}

func TestAuthorizeRequest_URL(t *testing.T) {
	req := oauth.AuthorizeRequest{
		ClientID:              "client",
		RedirectURI:           "https://app.test/return",
		Scopes:                []string{"openid", "profile"},
		State:                 "/patient",
		PKCE:                  oauth.PKCE{Verifier: "v", Challenge: "c"},
		AssertedLoginIdentity: "identity",
		Silent:                true,
	}
	u, err := url.Parse(req.URL("https://login.test"))
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(u.Host+u.Path, "login.test/authorize"))
	assert.Check(t, cmp.DeepEqual(u.Query(), url.Values{
		"client_id":               {"client"},
		"response_type":           {"code"},
		"redirect_uri":            {"https://app.test/return"},
		"scope":                   {"openid profile"},
		"state":                   {"/patient"},
		"code_challenge":          {"c"},
		"code_challenge_method":   {"S256"},
		"vtr":                     {`["P5.Cp.Cd","P5.Cp.Ck","P5.Cm","P9.Cp.Cd","P9.Cp.Ck","P9.Cm"]`},
		"asserted_login_identity": {"identity"},
		"prompt":                  {"none"},
	}))
}

func TestCode(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		code    string
		wantErr string
	}{
		{name: "code", query: "code=abc&state=x", code: "abc"},
		{name: "login required", query: "error=login_required", wantErr: oauth.ErrInteractionRequired.Error()},
		{name: "other error", query: "error=access_denied&error_description=nope", wantErr: "NHS login: access_denied: nope"},
		{name: "no code", query: "state=x", wantErr: "NHS login redirected without an authorization code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := oauth.Code(&url.URL{RawQuery: tt.query})
			if tt.wantErr != "" {
				assert.Check(t, cmp.Error(err, tt.wantErr))
				return
			}
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(code, tt.code))
		})
	}
}

func TestHTTPAuthorizer(t *testing.T) {
	nhs := fakenhs.New(t)
	redirectURI := fakenhs.WebURL + "/auth-return"
	authURL := oauth.AuthorizeRequest{
		ClientID:    fakenhs.ClientID,
		RedirectURI: redirectURI,
		State:       "/patient",
		PKCE:        oauth.NewPKCE(),
	}.URL(nhs.LoginURL())

	t.Run("catches the redirect back without following it", func(t *testing.T) {
		got, err := oauth.HTTPAuthorizer{}.Authorize(t.Context(), authURL, redirectURI)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(got.Scheme+"://"+got.Host+got.Path, redirectURI))
		assert.Check(t, cmp.Equal(got.Query().Get("state"), "/patient"))
		code, err := oauth.Code(got)
		assert.NilError(t, err)
		assert.Check(t, code != "")
	})

	t.Run("a login page needs the user", func(t *testing.T) {
		nhs.SetInteractive(true)
		_, err := oauth.HTTPAuthorizer{}.Authorize(t.Context(), authURL, redirectURI)
		assert.Check(t, cmp.ErrorIs(err, oauth.ErrInteractionRequired))
	})

	t.Run("errors leave out the URL", func(t *testing.T) {
		_, err := oauth.HTTPAuthorizer{}.Authorize(t.Context(), nhs.LoginURL()+"/authorize?code_challenge=secret", redirectURI)
		assert.Check(t, cmp.Error(err, "authorize: NHS login returned 400 Bad Request"))
	})
}
