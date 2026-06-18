package git

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jahrik/repo-sync/internal/testutil"
)

// systemPath is a minimal PATH that covers the real git binary location.
const systemPath = "/usr/bin:/bin:/usr/local/bin"

// NOTE: testutil.FakeGitScript uses `printf '%s'` in bash which does NOT
// interpret \n escape sequences.  That means Stdout strings with actual
// newlines (0x0A) are rendered as literal \n in the generated script and
// will NOT produce multi-line output.  For functions that call
// strings.Split(out, "\n"), each fake invocation must therefore only need
// to match a single-line output.

// TestAheadBehindParseError exercises the parse-error branch in AheadBehind
// where git outputs two whitespace-separated fields that are not integers.
// We use two space-separated words (spaces survive FakeGitScript's %q quoting)
// so strings.Fields gives two elements, both non-integer.
func TestAheadBehindParseError(t *testing.T) {
	dir := t.TempDir()
	// Use a space rather than \t so that the tab escape isn't mangled by %q.
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"rev-list --left-right --count origin/main...main": {
			Stdout:   "bad bad",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	_, _, err := r.AheadBehind(dir, "main", "main")
	if err == nil {
		t.Error("expected parse error from AheadBehind when git outputs non-integers")
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Errorf("error message = %q, want it to contain 'parse error'", err.Error())
	}
}

// TestAheadBehindWrongFieldCount exercises the unexpected-output branch in
// AheadBehind where git outputs a single field instead of two tab-separated values.
func TestAheadBehindWrongFieldCount(t *testing.T) {
	dir := t.TempDir()
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"rev-list --left-right --count origin/main...main": {
			Stdout:   "42",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	_, _, err := r.AheadBehind(dir, "main", "main")
	if err == nil {
		t.Error("expected error from AheadBehind when git outputs single field")
	}
	if !strings.Contains(err.Error(), "unexpected output") {
		t.Errorf("error message = %q, want it to contain 'unexpected output'", err.Error())
	}
}

// TestAheadBehindParseFirstIntError exercises strconv.Atoi failure on
// parts[0] while parts[1] is a valid integer.
// We use a space separator so %q quoting in FakeGitScript doesn't mangle the delimiter.
func TestAheadBehindParseFirstIntError(t *testing.T) {
	dir := t.TempDir()
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"rev-list --left-right --count origin/main...main": {
			Stdout:   "abc 3",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	_, _, err := r.AheadBehind(dir, "main", "main")
	if err == nil {
		t.Error("expected error from AheadBehind with non-integer first field")
	}
}

// TestAheadBehindParseSecondIntError exercises strconv.Atoi failure on
// parts[1] while parts[0] is a valid integer.
func TestAheadBehindParseSecondIntError(t *testing.T) {
	dir := t.TempDir()
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"rev-list --left-right --count origin/main...main": {
			Stdout:   "3 abc",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	_, _, err := r.AheadBehind(dir, "main", "main")
	if err == nil {
		t.Error("expected error from AheadBehind with non-integer second field")
	}
}

// TestGoneBranchesCurrentBranchWithAsterisk exercises the code path where
// the current branch (marked with *) shows as gone.
// The fake git outputs a single line (FakeGitScript limitation: no real newlines).
func TestGoneBranchesCurrentBranchWithAsterisk(t *testing.T) {
	dir := t.TempDir()
	// Single-line output: current branch with * and gone upstream.
	// strings.Split on "\n" with a single-line string gives one element.
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"branch -vv": {
			Stdout:   "* gone-feat  abc123 [origin/gone-feat: gone] some commit",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	branches, err := r.GoneBranches(dir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	found := false
	for _, b := range branches {
		if b == "gone-feat" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected gone-feat in result, got %v", branches)
	}
}

// TestGoneBranchesAsteriskNamedBranch verifies that when the current branch (*)
// appears gone, we return fields[1] (branch name), not fields[0] ("*").
func TestGoneBranchesAsteriskNamedBranch(t *testing.T) {
	dir := t.TempDir()
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"branch -vv": {
			Stdout:   "* my-branch  deadbeef [origin/my-branch: gone] add feature",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	branches, err := r.GoneBranches(dir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	if len(branches) != 1 || branches[0] != "my-branch" {
		t.Errorf("GoneBranches = %v, want [my-branch]", branches)
	}
}

// TestGoneBranchesNonCurrentGoneBranch exercises the normal (non-asterisk)
// gone branch detection path in GoneBranches.
func TestGoneBranchesNonCurrentGoneBranch(t *testing.T) {
	dir := t.TempDir()
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"branch -vv": {
			Stdout:   "  stale-feat  abc123 [origin/stale-feat: gone] old work",
			ExitCode: 0,
		},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	t.Setenv("PATH", binDir+":"+systemPath)

	r := NewRunner()
	branches, err := r.GoneBranches(dir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	if len(branches) != 1 || branches[0] != "stale-feat" {
		t.Errorf("GoneBranches = %v, want [stale-feat]", branches)
	}
}

// TestWriteFakeBinaryProducesExecutable verifies that WriteFakeBinary creates
// an executable that runs correctly and produces the expected output.
func TestWriteFakeBinaryProducesExecutable(t *testing.T) {
	script := testutil.FakeGitScript(map[string]testutil.FakeOutput{
		"version": {Stdout: "git version fake", ExitCode: 0},
	})
	binDir := testutil.WriteFakeBinary(t, script)
	cmd := exec.Command(binDir+"/git", "version")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fake binary not executable or returned error: %v", err)
	}
	if strings.TrimSpace(string(out)) != "git version fake" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(string(out)), "git version fake")
	}
}
