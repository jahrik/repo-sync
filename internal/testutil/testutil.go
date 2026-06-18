package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeOutput holds what a fake git subcommand should print and whether it
// should exit non-zero.
type FakeOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// FakeGitScript generates a bash case-statement script that dispatches on
// "$1" (the git subcommand) and returns the configured output/exit-code.
// subcmdOutputs keys may be the first argument only (e.g., "fetch") or a
// space-separated sequence of arguments (e.g., "rev-parse --git-dir").
func FakeGitScript(subcmdOutputs map[string]FakeOutput) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\ncase \"$*\" in\n")
	for key, out := range subcmdOutputs {
		// Escape single-quotes in the key.
		escaped := strings.ReplaceAll(key, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("  '%s')\n", escaped))
		if out.Stdout != "" {
			sb.WriteString(fmt.Sprintf("    printf '%%s' %q\n", out.Stdout))
		}
		if out.Stderr != "" {
			sb.WriteString(fmt.Sprintf("    printf '%%s' %q >&2\n", out.Stderr))
		}
		sb.WriteString(fmt.Sprintf("    exit %d\n    ;;\n", out.ExitCode))
	}
	sb.WriteString("  *)\n    echo \"fake-git: unknown args: $*\" >&2\n    exit 1\n    ;;\nesac\n")
	return sb.String()
}

// WriteFakeBinary writes script to a temporary directory as an executable
// named "git" and returns the directory path suitable for prepending to PATH.
func WriteFakeBinary(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "git")
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("testutil: write fake git: %v", err)
	}
	return dir
}
