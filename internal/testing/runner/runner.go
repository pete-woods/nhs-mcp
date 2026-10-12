// Package runner runs the nhs-mcp binary for acceptance tests, wired to the
// fake NHS, and connects an MCP client to it over stdio.
package runner

import (
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/testing/egress"
	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
)

type Binary struct {
	path string
	env  []string
	trap *egress.Trap
}

// New prepares to run binary against nhs. The environment is built from
// scratch, and logins follow redirects over HTTP rather than open a browser.
func New(t *testing.T, binary string, nhs *fakenhs.Server) *Binary {
	trap := egress.New(t)
	home := t.TempDir()
	env := slices.Concat([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + home + "/.config",
		"NHS_MCP_AUTH_URL=" + nhs.LoginURL(),
		"NHS_MCP_API_URL=" + nhs.AppURL(),
		"NHS_MCP_WEB_URL=" + fakenhs.WebURL,
		"NHS_MCP_LOGIN=http",
		"NHS_MCP_DEBUG=1",
	}, trap.Env())
	return &Binary{path: binary, env: env, trap: trap}
}

// With returns a copy that also sets environment (NAME=value; NAME= unsets).
func (b *Binary) With(environment ...string) *Binary {
	c := *b
	c.env = slices.Concat(b.env, environment)
	return &c
}

// Escaped takes the requests that tried to leave the machine.
func (b *Binary) Escaped() []egress.Request {
	return b.trap.Take()
}

// Connect starts the binary and returns an MCP session to it. The binary's
// logs go to the test log.
func (b *Binary) Connect(t *testing.T) *sdk.ClientSession {
	cmd := exec.Command(b.path)
	cmd.Env = b.env
	stderr := &testWriter{out: t.Output()}
	cmd.Stderr = stderr
	t.Cleanup(stderr.close)
	return mcptest.Session(t, &sdk.CommandTransport{Command: cmd})
}

// testWriter drops writes once the test has ended, when writing to its output
// would panic.
type testWriter struct {
	mu     sync.Mutex
	out    io.Writer
	closed bool
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	return w.out.Write(p)
}

func (w *testWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
}
