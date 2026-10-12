package acceptance_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/pete-woods/nhs-mcp/internal/testing/compiler"
)

var binary string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	bin, cleanup, err := compiler.Build(context.Background(), "..", ".", "nhs-mcp")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	binary = bin
	return m.Run()
}
