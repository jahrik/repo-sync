package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRepo creates a minimal git repo in a temp dir for testing.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Create a commit so HEAD is not detached.
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestIsGitRepo(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)
	if !r.IsGitRepo(dir) {
		t.Error("expected IsGitRepo=true for initialised repo")
	}
	if r.IsGitRepo(t.TempDir()) {
		t.Error("expected IsGitRepo=false for plain dir")
	}
}

func TestCurrentBranch(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)
	branch, err := r.CurrentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
}

func TestStatusDirty(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	dirty, err := r.StatusDirty(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("expected clean repo after commit")
	}

	// Make it dirty.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("change"), 0644); err != nil {
		t.Fatal(err)
	}
	dirty, err = r.StatusDirty(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("expected dirty repo after adding untracked file")
	}
}

func TestMergedBranches(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	// Create a branch that is immediately merged (no new commits).
	cmd := exec.Command("git", "branch", "feature-merged")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create branch: %v: %s", err, out)
	}

	// Add a fake origin remote pointing to itself.
	cmd2 := exec.Command("git", "remote", "add", "origin", dir)
	cmd2.Dir = dir
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("add remote: %v: %s", err, out)
	}
	cmd3 := exec.Command("git", "fetch", "origin")
	cmd3.Dir = dir
	if out, err := cmd3.CombinedOutput(); err != nil {
		t.Fatalf("fetch: %v: %s", err, out)
	}

	branches, err := r.MergedBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range branches {
		if b == "feature-merged" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected feature-merged in merged branches, got %v", branches)
	}
}

func TestRemoteURLNotGitHub(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	// Add a non-github remote.
	cmd := exec.Command("git", "remote", "add", "origin", "https://gitlab.com/foo/bar.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add remote: %v: %s", err, out)
	}

	_, err := r.RemoteURL(dir)
	if err != ErrNotGitHub {
		t.Errorf("expected ErrNotGitHub, got %v", err)
	}
}
