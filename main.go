// Command nhs-mcp is an unofficial MCP server for the NHS App, run over stdio.
//
// Configuration is all environment variables:
//
//	NHS_MCP_LOGIN        "browser" (default) to log in in a Chrome window, or
//	                     "http" to follow NHS login's redirects without one
//	NHS_MCP_BROWSER      Chrome binary; found or downloaded when unset
//	NHS_MCP_PROFILE_DIR  browser profile, kept between logins
//	NHS_MCP_AUTH_URL     NHS login (https://auth.login.nhs.uk)
//	NHS_MCP_API_URL      NHS App API (https://api.nhsapp.service.nhs.uk)
//	NHS_MCP_WEB_URL      NHS App web app (https://www.nhsapp.service.nhs.uk)
//	NHS_MCP_DEBUG        log at debug level when set
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/api/mcp"
	"github.com/pete-woods/nhs-mcp/internal/browser"
	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/logging"
	"github.com/pete-woods/nhs-mcp/internal/oauth"
	"github.com/pete-woods/nhs-mcp/internal/session"
)

func main() {
	os.Exit(exitCode())
}

func exitCode() int {
	logging.Setup(os.Getenv("NHS_MCP_DEBUG") != "")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("nhs-mcp failed", "error", err)
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	authorizer, err := newAuthorizer()
	if err != nil {
		return err
	}
	app := nhsapp.New(nhsapp.Config{
		APIURL: env("NHS_MCP_API_URL", "https://api.nhsapp.service.nhs.uk"),
		WebURL: env("NHS_MCP_WEB_URL", "https://www.nhsapp.service.nhs.uk"),
	})
	sessions := session.New(session.Config{
		App:        app,
		Authorizer: authorizer,
		AuthURL:    env("NHS_MCP_AUTH_URL", "https://auth.login.nhs.uk"),
	})
	defer func() { _ = sessions.Logout(context.WithoutCancel(ctx)) }()

	server := mcp.New(mcp.Config{Sessions: sessions, Version: version()})
	slog.InfoContext(ctx, "nhs-mcp started", "version", version())
	return server.Run(ctx, &sdk.StdioTransport{})
}

func newAuthorizer() (oauth.Authorizer, error) {
	switch mode := env("NHS_MCP_LOGIN", "browser"); mode {
	case "browser":
		profile := os.Getenv("NHS_MCP_PROFILE_DIR")
		if profile == "" {
			dir, err := os.UserConfigDir()
			if err != nil {
				return nil, fmt.Errorf("find a home for the browser profile (set NHS_MCP_PROFILE_DIR): %w", err)
			}
			profile = filepath.Join(dir, "nhs-mcp", "browser")
		}
		return browser.New(browser.Config{Bin: os.Getenv("NHS_MCP_BROWSER"), ProfileDir: profile}), nil
	case "http":
		return oauth.HTTPAuthorizer{}, nil
	default:
		return nil, fmt.Errorf("NHS_MCP_LOGIN must be browser or http, not %q", mode)
	}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}
