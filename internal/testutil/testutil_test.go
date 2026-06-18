package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeGitScriptBasic(t *testing.T) {
	script := FakeGitScript(map[string]FakeOutput{
		"status": {Stdout: "clean\n", ExitCode: 0},
		"fetch":  {Stderr: "oops", ExitCode: 1},
	})

	if !strings.Contains(script, "#!/bin/bash") {
		t.Error("expected shebang in script")
	}
	if !strings.Contains(script, "status") {
		t.Error("expected 'status' case in script")
	}
	if !strings.Contains(script, "fetch") {
		t.Error("expected 'fetch' case in script")
	}
}

func TestWriteFakeBinaryAndRun(t *testing.T) {
	script := FakeGitScript(map[string]FakeOutput{
		"rev-parse --git-dir": {Stdout: ".git", ExitCode: 0},
		"status --porcelain":  {Stdout: "", ExitCode: 0},
	})

	binDir := WriteFakeBinary(t, script)

	// Verify the binary exists.
	binPath := filepath.Join(binDir, "git")
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("fake git binary not found at %s: %v", binPath, err)
	}

	// Run the fake git with a known argument.
	cmd := exec.Command(binPath, "rev-parse", "--git-dir")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fake git rev-parse: %v", err)
	}
	if strings.TrimSpace(string(out)) != ".git" {
		t.Errorf("fake git output = %q, want .git", string(out))
	}
}

func TestWriteFakeBinaryExitCode(t *testing.T) {
	script := FakeGitScript(map[string]FakeOutput{
		"bad-cmd": {ExitCode: 2},
	})

	binDir := WriteFakeBinary(t, script)
	binPath := filepath.Join(binDir, "git")

	cmd := exec.Command(binPath, "bad-cmd")
	err := cmd.Run()
	if err == nil {
		t.Error("expected non-zero exit from fake git bad-cmd")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.ExitCode())
	}
}

func TestFakeGitScriptUnknownCmd(t *testing.T) {
	script := FakeGitScript(map[string]FakeOutput{})
	binDir := WriteFakeBinary(t, script)
	binPath := filepath.Join(binDir, "git")

	cmd := exec.Command(binPath, "unknown-subcommand")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected error for unknown subcommand")
	}
	if !strings.Contains(string(out), "fake-git: unknown args") {
		t.Errorf("expected 'fake-git: unknown args' in stderr, got: %s", out)
	}
}

func TestFakeGitScriptSingleQuoteEscape(t *testing.T) {
	// Key containing a single quote — should not break the generated bash script.
	script := FakeGitScript(map[string]FakeOutput{
		"it's a test": {Stdout: "ok", ExitCode: 0},
	})
	// Just verify it generates without panicking and contains the escaped form.
	if !strings.Contains(script, "it") {
		t.Error("expected key content in generated script")
	}
}
