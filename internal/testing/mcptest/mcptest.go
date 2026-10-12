// Package mcptest connects an MCP client to a server for tests, and calls
// its tools.
package mcptest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

// Timeout bounds each call.
const Timeout = 30 * time.Second

// Connect serves server in memory and returns a client session to it.
func Connect(t *testing.T, server *sdk.Server) *sdk.ClientSession {
	t.Helper()
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), serverT, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	return Session(t, clientT)
}

// Session connects a client over transport.
func Session(t *testing.T, transport sdk.Transport) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), Timeout)
	defer cancel()
	cs, err := client.Connect(ctx, transport, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// Result calls a tool and returns its raw result, failed or not.
func Result(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), Timeout)
	defer cancel()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	assert.NilError(t, err)
	return res
}

// Call calls a tool that must succeed, decoding its structured output into T.
func Call[T any](t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res := Result(t, cs, name, args)
	assert.Assert(t, !res.IsError, "%s failed: %s", name, text(res))
	raw, err := json.Marshal(res.StructuredContent)
	assert.NilError(t, err)
	var out T
	assert.NilError(t, json.Unmarshal(raw, &out))
	return out
}

// Error calls a tool that must fail, returning its error message.
func Error(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res := Result(t, cs, name, args)
	assert.Assert(t, res.IsError, "%s should have failed", name)
	return text(res)
}

func text(res *sdk.CallToolResult) string {
	var s string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			s += tc.Text
		}
	}
	return s
}
