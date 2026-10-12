// Package logging sets up slog. Logs always go to stderr, since stdout carries
// the MCP protocol.
package logging

import (
	"io"
	"log/slog"
	"os"
)

// New returns a text logger writing to w, at debug level when debug is set.
func New(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// Setup makes a stderr logger the default for slog and the log package.
func Setup(debug bool) {
	slog.SetDefault(New(os.Stderr, debug))
}
