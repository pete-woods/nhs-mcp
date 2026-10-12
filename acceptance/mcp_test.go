package acceptance_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/testing/egress"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
	"github.com/pete-woods/nhs-mcp/internal/testing/runner"
)

func TestE2E(t *testing.T) {
	nhs := fakenhs.New(t)
	cs := runner.New(t, binary, nhs).Connect(t)

	t.Run("instructions", func(t *testing.T) {
		assert.Check(t, cmp.Contains(cs.InitializeResult().Instructions, "Call login first"))
	})

	t.Run("tools", func(t *testing.T) {
		res, err := cs.ListTools(t.Context(), nil)
		assert.NilError(t, err)
		names := make([]string, 0, len(res.Tools))
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		assert.Check(t, cmp.DeepEqual(names, []string{
			"get_allergies", "get_demographics", "get_immunisations", "get_login_status",
			"get_medications", "get_problems", "get_test_result", "get_test_trend",
			"list_test_names", "list_test_results", "login", "logout",
		}))
	})

	t.Run("login", func(t *testing.T) {
		got := mcptest.Call[map[string]any](t, cs, "login", nil)
		assert.Check(t, cmp.Equal(got["logged_in"], true))
		assert.Check(t, cmp.Equal(got["gp_session"], true))
	})

	t.Run("out of range results", func(t *testing.T) {
		got := mcptest.Call[struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}](t, cs, "list_test_results", map[string]any{"out_of_range_only": true})
		assert.Assert(t, cmp.Len(got.Results, 2))
		assert.Check(t, cmp.Equal(got.Results[0].ID, "result-fbc-2024"))
	})

	t.Run("trend", func(t *testing.T) {
		got := mcptest.Call[struct {
			Points []struct {
				Value string `json:"value"`
			} `json:"points"`
		}](t, cs, "get_test_trend", map[string]any{"name": "Serum ferritin"})
		assert.Assert(t, cmp.Len(got.Points, 2))
		assert.Check(t, cmp.Equal(got.Points[1].Value, "22 ug/L"))
	})

	t.Run("logout", func(t *testing.T) {
		mcptest.Call[map[string]any](t, cs, "logout", nil)
		assert.Check(t, cmp.Equal(nhs.Sessions(), 0))
	})
}

func TestE2E_defaultsToTheRealNHS(t *testing.T) {
	nhs := fakenhs.New(t)
	run := runner.New(t, binary, nhs).With("NHS_MCP_AUTH_URL=", "NHS_MCP_API_URL=", "NHS_MCP_WEB_URL=")
	cs := run.Connect(t)

	mcptest.Error(t, cs, "login", nil)
	assert.Check(t, cmp.DeepEqual(run.Escaped(), []egress.Request{{Method: "CONNECT", Host: "auth.login.nhs.uk:443"}}))
}
