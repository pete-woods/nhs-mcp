// Package testlog sends slog output to the test's log.
package testlog

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/pete-woods/nhs-mcp/internal/logging"
)

// Capture makes the default slog logger write to t's output at debug level,
// until the test ends.
func Capture(t testing.TB) {
	w := &writer{out: t.Output()}
	prev := slog.Default()
	slog.SetDefault(logging.New(w, true))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		w.close()
	})
}

// writer drops writes after the test ends: a goroutine may still log on its
// way out, and writing to a finished test's output panics.
type writer struct {
	mu     sync.Mutex
	out    io.Writer
	closed bool
}

func (w *writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	return w.out.Write(p)
}

func (w *writer) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
}
