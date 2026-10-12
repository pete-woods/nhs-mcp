package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/session"
)

func (a *api) addAuthTools(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "login",
		Description: `Log in to the NHS App. Opens a browser window at NHS login, where the user signs in with their own email, password and security code; this waits (up to five minutes) until they finish.
Returns the same status as get_login_status. Ask the user before calling it, and tell them to look for the window.`,
		Annotations: &sdk.ToolAnnotations{Title: "Log in to the NHS App", DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, a.login)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_login_status",
		Description: `Whether the user is logged in to the NHS App, and as whom (the record's name, GP practice ODS code, and NHS login proof level).`,
		Annotations: &sdk.ToolAnnotations{Title: "Get login status", ReadOnlyHint: true},
	}, a.loginStatus)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "logout",
		Description: `Log out of the NHS App and forget the cached record.`,
		Annotations: &sdk.ToolAnnotations{Title: "Log out", DestructiveHint: ptr(false), IdempotentHint: true},
	}, a.logout)
}

type statusOutput struct {
	LoggedIn   bool   `json:"logged_in"`
	GPSession  bool   `json:"gp_session" jsonschema:"Whether the GP record can be read yet. It starts on demand if not."`
	Name       string `json:"name,omitempty"`
	ODSCode    string `json:"ods_code,omitempty" jsonschema:"The GP practice's ODS code."`
	ProofLevel string `json:"proof_level,omitempty" jsonschema:"NHS login identity proofing: P9 is full access, P5 is limited."`
}

func (a *api) login(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *statusOutput, error) {
	st, err := a.sessions.Login(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	return nil, toStatus(st), nil
}

func (a *api) loginStatus(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *statusOutput, error) {
	st, err := a.sessions.Status(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	return nil, toStatus(st), nil
}

type logoutOutput struct {
	LoggedOut bool `json:"logged_out"`
}

func (a *api) logout(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *logoutOutput, error) {
	if err := a.sessions.Logout(ctx); err != nil {
		return nil, nil, userError(err)
	}
	return nil, &logoutOutput{LoggedOut: true}, nil
}

func toStatus(st session.Status) *statusOutput {
	return &statusOutput{
		LoggedIn:   st.LoggedIn,
		GPSession:  st.GPSession,
		Name:       st.Name,
		ODSCode:    st.ODSCode,
		ProofLevel: st.ProofLevel,
	}
}
