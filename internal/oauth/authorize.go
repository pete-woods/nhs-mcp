package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The vectors of trust the NHS App accepts: identity proofing P5 or P9, with
// any credential type.
const vtr = `["P5.Cp.Cd","P5.Cp.Ck","P5.Cm","P9.Cp.Cd","P9.Cp.Ck","P9.Cm"]`

// AuthorizeRequest describes an authorization request to NHS login.
type AuthorizeRequest struct {
	ClientID    string
	RedirectURI string
	Scopes      []string
	State       string
	PKCE        PKCE
	// AssertedLoginIdentity is a token from the relying party that lets NHS
	// login sign the user in again without interaction.
	AssertedLoginIdentity string
	// Silent asks NHS login not to show a login page (prompt=none).
	Silent bool
}

// URL is the authorize URL under baseURL, e.g. https://auth.login.nhs.uk.
func (r AuthorizeRequest) URL(baseURL string) string {
	q := url.Values{}
	q.Set("client_id", r.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", r.RedirectURI)
	q.Set("scope", strings.Join(r.Scopes, " "))
	q.Set("state", r.State)
	q.Set("code_challenge", r.PKCE.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("vtr", vtr)
	if r.AssertedLoginIdentity != "" {
		q.Set("asserted_login_identity", r.AssertedLoginIdentity)
	}
	if r.Silent {
		q.Set("prompt", "none")
	}
	return baseURL + "/authorize?" + q.Encode()
}

// Authorizer takes the user through an authorize URL until NHS login
// redirects to redirectURI, and returns that redirect without loading it.
type Authorizer interface {
	Authorize(ctx context.Context, authorizeURL, redirectURI string) (*url.URL, error)
}

// ErrInteractionRequired means NHS login wants the user to sign in, which the
// Authorizer can't do.
var ErrInteractionRequired = errors.New("NHS login needs the user to sign in")

// Code extracts the authorization code from the redirect, or the error NHS
// login sent instead.
func Code(redirect *url.URL) (string, error) {
	q := redirect.Query()
	if e := q.Get("error"); e != "" {
		if e == "login_required" || e == "interaction_required" {
			return "", ErrInteractionRequired
		}
		return "", fmt.Errorf("NHS login: %s: %s", e, q.Get("error_description"))
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("NHS login redirected without an authorization code")
	}
	return code, nil
}
