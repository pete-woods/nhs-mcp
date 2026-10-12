package browser_test

import (
	"context"
	"os"
	"time"

	"testing"

	"github.com/go-rod/rod/lib/launcher"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/browser"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
)

func TestAuthorizer(t *testing.T) {
	bin, ok := launcher.LookPath()
	if !ok || testing.Short() {
		t.Skip("needs Chrome or Chromium installed")
	}

	nhs := fakenhs.New(t)
	a := browser.New(browser.Config{Bin: bin, ProfileDir: t.TempDir(), Headless: true, NoSandbox: os.Getenv("CI") != ""})
	t.Cleanup(func() { assert.Check(t, a.Close()) })

	redirectURI := fakenhs.WebURL + "/auth-return"
	authURL := oauth.AuthorizeRequest{
		ClientID:    fakenhs.ClientID,
		RedirectURI: redirectURI,
		State:       "/patient",
		PKCE:        oauth.NewPKCE(),
	}.URL(nhs.LoginURL())

	// Twice, to check the browser and its window are reused between
	// authorizations.
	for range 2 {
		got, err := a.Authorize(t.Context(), authURL, redirectURI)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(got.Host, "www.nhsapp.test"))
		code, err := oauth.Code(got)
		assert.NilError(t, err)
		assert.Check(t, code != "")
	}
	pages, err := a.Pages()
	assert.NilError(t, err)
	assert.Check(t, cmp.Len(pages, 1))

	t.Run("opens a new window if the user closed it", func(t *testing.T) {
		pages, err := a.Pages()
		assert.NilError(t, err)
		assert.Assert(t, cmp.Len(pages, 1))
		assert.NilError(t, pages[0].Close())

		_, err = a.Authorize(t.Context(), authURL, redirectURI)
		assert.NilError(t, err)
		pages, err = a.Pages()
		assert.NilError(t, err)
		assert.Check(t, cmp.Len(pages, 1))
	})

	t.Run("waits for the user to sign in", func(t *testing.T) {
		nhs.SetInteractive(true)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		_, err := a.Authorize(ctx, authURL, redirectURI)
		assert.Check(t, cmp.ErrorIs(err, context.DeadlineExceeded))
	})
}
