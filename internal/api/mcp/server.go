// Package mcp is the MCP server: tools that let an agent log in to the NHS App
// and read the user's GP record, test results above all.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/session"
)

const instructions = `Reads the user's own GP record through the NHS App. Unofficial: it uses the NHS App's web API, not a supported one.

Call login first. It opens a browser window where the user signs in to NHS login themselves; it waits until they finish, which can take a few minutes. Sessions end after about ten minutes idle, after which tools say so and login is needed again.

Consultations and documents: list_consultations shows what was recorded at each appointment; list_documents lists letters and scans, and get_document reads one, as text or as page images.

For test results: list_test_results finds results (filter by date, name, or out_of_range_only); get_test_result shows every value in one; get_test_trend follows one measurement (e.g. "Serum ferritin") over the years; list_test_names gives the exact names to use.

Ranges and flags come from the GP system. Out-of-range doesn't always mean something is wrong — leave clinical judgement to the user's clinicians.`

type Config struct {
	Sessions *session.Manager
	Version  string
}

func New(cfg Config) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "nhs-app", Version: cfg.Version}, &sdk.ServerOptions{
		Instructions: instructions,
		// Set explicitly so the SDK doesn't advertise listChanged, which makes
		// some clients hold a stream open waiting for changes that never come.
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}, Resources: &sdk.ResourceCapabilities{}},
	})
	a := &api{sessions: cfg.Sessions}
	a.addAuthTools(s)
	a.addResultTools(s)
	a.addRecordTools(s)
	a.addDocumentTools(s)
	return s
}

type api struct {
	sessions *session.Manager
}

// addTool adds a tool whose result is only its structured output. Left to
// itself, the SDK repeats that output as JSON text, and clients that get both
// handle it badly. Handlers that return a result keep its content, so tools
// that return files can.
func addTool[In, Out any](s *sdk.Server, t *sdk.Tool, h sdk.ToolHandlerFor[In, Out]) {
	sdk.AddTool(s, t, func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if res == nil {
			res = &sdk.CallToolResult{Content: []sdk.Content{}}
		}
		return res, out, err
	})
}

// userError explains an error in terms the agent can act on.
func userError(err error) error {
	switch {
	case errors.Is(err, nhsapp.ErrNotLoggedIn):
		return errors.New("not logged in to the NHS App, or the session expired: call login")
	case errors.Is(err, oauth.ErrInteractionRequired):
		return errors.New("NHS login needs the user to sign in, which this server can't show without a browser (set NHS_MCP_LOGIN=browser)")
	}
	return err
}

// parseDate parses an optional YYYY-MM-DD date argument.
func parseDate(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a date like 2024-01-31: %w", name, err)
	}
	return t, nil
}

// sectionWarning explains a part of the record that couldn't be read in full.
func sectionWarning(what string, s nhsapp.SectionStatus) string {
	switch {
	case s.HasErrored:
		return fmt.Sprintf("The GP system returned an error for %s, so this may be incomplete.", what)
	case !s.HasAccess:
		return fmt.Sprintf("The GP practice doesn't share %s online with this account.", what)
	}
	return ""
}

func clip[T any](items []T, limit, def, maxLimit int) ([]T, bool) {
	if limit <= 0 {
		limit = def
	}
	limit = min(limit, maxLimit)
	if len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}

func ptr[T any](v T) *T {
	return &v
}
