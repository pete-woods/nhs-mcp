package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/pete-woods/nhs-mcp/internal/httpcl"
)

// HTTPAuthorizer follows NHS login's redirects with plain HTTP. It only
// succeeds when NHS login needs nothing from the user, as for a silent
// re-authorization with an asserted login identity.
type HTTPAuthorizer struct {
	// Transport overrides the HTTP transport (useful for testing).
	Transport http.RoundTripper
}

func (a HTTPAuthorizer) Authorize(ctx context.Context, authorizeURL, redirectURI string) (*url.URL, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	cl := httpcl.New(httpcl.Config{
		Transport: a.Transport,
		Jar:       jar,
		// Retrying would replay a redirect chain that has already used its codes.
		DisableRetries: true,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.HasPrefix(req.URL.String(), redirectURI) {
				return &redirected{to: req.URL}
			}
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	})

	var page string
	_, err = cl.Call(ctx, httpcl.NewRequest(http.MethodGet, authorizeURL, httpcl.StringDecoder(&page)))
	if r, ok := errors.AsType[*redirected](err); ok {
		return r.to, nil
	}
	if he, ok := errors.AsType[*httpcl.HTTPError](err); ok {
		return nil, fmt.Errorf("authorize: NHS login returned %d %s", he.StatusCode, http.StatusText(he.StatusCode))
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		// Unwrapped, so the error doesn't repeat the URL and its tokens.
		return nil, fmt.Errorf("authorize: %w", ue.Err)
	}
	if err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}
	// NHS login answered with a page (the login form) instead of redirecting.
	return nil, ErrInteractionRequired
}

type redirected struct {
	to *url.URL
}

func (r *redirected) Error() string {
	return "redirected to " + r.to.Redacted()
}
