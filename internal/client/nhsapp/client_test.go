package nhsapp_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
)

func TestClient(t *testing.T) {
	nhs := fakenhs.New(t)
	c := nhsapp.New(nhsapp.Config{APIURL: nhs.AppURL(), WebURL: fakenhs.WebURL})
	ctx := t.Context()

	t.Run("calls need a session", func(t *testing.T) {
		_, err := c.Session(ctx)
		assert.Check(t, cmp.ErrorIs(err, nhsapp.ErrNotLoggedIn))
	})

	t.Run("a code needs its own verifier", func(t *testing.T) {
		code := authorize(t, nhs, c.LoginRedirectURI(), oauth.NewPKCE(), "")
		_, err := c.CreateSession(ctx, nhsapp.AuthCode{Code: code, Verifier: oauth.NewPKCE().Verifier, RedirectURI: c.LoginRedirectURI()})
		assert.Check(t, cmp.ErrorContains(err, "400 Bad Request"))
	})

	pkce := oauth.NewPKCE()
	code := authorize(t, nhs, c.LoginRedirectURI(), pkce, "")
	s, err := c.CreateSession(ctx, nhsapp.AuthCode{Code: code, Verifier: pkce.Verifier, RedirectURI: c.LoginRedirectURI()})
	assert.NilError(t, err)

	t.Run("session", func(t *testing.T) {
		assert.Check(t, cmp.Equal(s.Name, fakenhs.Name))
		assert.Check(t, !s.HasGPSession)
		got, err := c.Session(ctx)
		assert.NilError(t, err)
		assert.Check(t, cmp.DeepEqual(got, s))
		assert.Check(t, c.ExtendSession(ctx))
	})

	t.Run("a code is spent once", func(t *testing.T) {
		_, err := c.CreateSession(ctx, nhsapp.AuthCode{Code: code, Verifier: pkce.Verifier, RedirectURI: c.LoginRedirectURI()})
		assert.Check(t, cmp.ErrorContains(err, "400 Bad Request"))
	})

	t.Run("the record needs a GP session", func(t *testing.T) {
		_, err := c.MyRecord(ctx)
		assert.Check(t, cmp.ErrorIs(err, nhsapp.ErrNoGPSession))
	})

	t.Run("GP session", func(t *testing.T) {
		identity, err := c.AssertedLoginIdentity(ctx)
		assert.NilError(t, err)
		pkce := oauth.NewPKCE()
		code := authorize(t, nhs, c.GPRedirectURI(), pkce, identity)
		s, err := c.CreateGPSession(ctx, nhsapp.AuthCode{Code: code, Verifier: pkce.Verifier, RedirectURI: c.GPRedirectURI()})
		assert.NilError(t, err)
		assert.Check(t, s.HasGPSession)

		r, err := c.MyRecord(ctx)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(r.Supplier, "EMIS"))
		assert.Check(t, cmp.Len(r.TestResults.Data, 6))
		assert.Check(t, r.TestResults.HasAllHistoricResults)

		d, err := c.Demographics(ctx)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(d.NHSNumber, fakenhs.NHSNumber))
	})

	t.Run("logout", func(t *testing.T) {
		assert.NilError(t, c.DeleteSession(ctx))
		assert.Check(t, cmp.Equal(nhs.Sessions(), 0))
		_, err := c.Session(ctx)
		assert.Check(t, cmp.ErrorIs(err, nhsapp.ErrNotLoggedIn))
	})
}

func authorize(t *testing.T, nhs *fakenhs.Server, redirectURI string, pkce oauth.PKCE, identity string) string {
	t.Helper()
	authURL := oauth.AuthorizeRequest{
		ClientID:              nhsapp.ClientID,
		RedirectURI:           redirectURI,
		PKCE:                  pkce,
		AssertedLoginIdentity: identity,
		Silent:                identity != "",
	}.URL(nhs.LoginURL())
	u, err := oauth.HTTPAuthorizer{}.Authorize(t.Context(), authURL, redirectURI)
	assert.NilError(t, err)
	code, err := oauth.Code(u)
	assert.NilError(t, err)
	return code
}
