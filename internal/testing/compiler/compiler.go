// Package compiler builds binaries for acceptance tests.
package compiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Build compiles the main package at source, relative to dir, into a temporary
// directory, and returns the binary's path. Call the cleanup function when done.
func Build(ctx context.Context, dir, source, name string) (string, func(), error) {
	out, err := os.MkdirTemp("", "acceptance-binaries")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(out) }

	bin := filepath.Join(out, name)
	goBin := "go"
	if root := os.Getenv("GOROOT"); root != "" {
		goBin = filepath.Join(root, "bin", "go")
	}
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", bin, source)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("go build %s: %w\n%s", source, err, b)
	}
	return bin, cleanup, nil
}
