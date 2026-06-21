package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepoWithRemote creates a local repo with a self-pointing origin remote and
// fetches it so origin/HEAD, origin/main, etc. are set up.
func initRepoWithRemote(t *testing.T) (repoDir string) {
	t.Helper()
	dir := initRepo(t)

	for _, args := range [][]string{
		{"remote", "add", "origin", dir},
		{"fetch", "origin"},
		{"branch", "--set-upstream-to=origin/main", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestFetchPrune(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)

	if err := r.FetchPrune(dir); err != nil {
		t.Fatalf("FetchPrune: %v", err)
	}
}

func TestFetchPruneError(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t) // no remote → fetch will fail
	if err := r.FetchPrune(dir); err == nil {
		t.Error("expected error from FetchPrune with no remote")
	}
}

func TestDefaultBranchSymbolicRef(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)

	// Set origin/HEAD to point to main.
	cmd := exec.Command("git", "remote", "set-head", "origin", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set-head: %v: %s", err, out)
	}

	branch, err := r.DefaultBranch(dir)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("DefaultBranch = %q, want main", branch)
	}
}

func TestDefaultBranchShowRef(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)
	// Do NOT set origin/HEAD — force fallback to show-ref.
	// origin/main still exists from the fetch, so show-ref should succeed.
	branch, err := r.DefaultBranch(dir)
	if err != nil {
		t.Fatalf("DefaultBranch (show-ref path): %v", err)
	}
	if branch != "main" {
		t.Errorf("DefaultBranch = %q, want main", branch)
	}
}

func TestDefaultBranchFallbackMaster(t *testing.T) {
	r := NewRunner()
	// Create a repo that has no origin at all → both symbolic-ref and show-ref fail.
	dir := initRepo(t)
	branch, err := r.DefaultBranch(dir)
	if err != nil {
		t.Fatalf("DefaultBranch fallback: %v", err)
	}
	if branch != "master" {
		t.Errorf("DefaultBranch fallback = %q, want master", branch)
	}
}

func TestCurrentBranchError(t *testing.T) {
	r := NewRunner()
	// An empty dir is not a git repo so rev-parse will fail.
	dir := t.TempDir()
	_, err := r.CurrentBranch(dir)
	if err == nil {
		t.Error("expected error from CurrentBranch on non-git dir")
	}
}

func TestAheadBehind(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)

	// Should be 0/0 after fresh fetch.
	ahead, behind, err := r.AheadBehind(dir, "main", "main")
	if err != nil {
		t.Fatalf("AheadBehind: %v", err)
	}
	if ahead != 0 || behind != 0 {
		t.Errorf("ahead=%d behind=%d, want 0/0", ahead, behind)
	}
}

func TestAheadBehindError(t *testing.T) {
	r := NewRunner()
	dir := t.TempDir() // not a git repo
	_, _, err := r.AheadBehind(dir, "main", "main")
	if err == nil {
		t.Error("expected error from AheadBehind on non-git dir")
	}
}

func TestPullFFOnly(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)

	if err := r.PullFFOnly(dir); err != nil {
		t.Fatalf("PullFFOnly: %v", err)
	}
}

func TestPullFFOnlyError(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t) // no remote → pull will fail
	if err := r.PullFFOnly(dir); err == nil {
		t.Error("expected error from PullFFOnly with no remote")
	}
}

func TestGoneBranches(t *testing.T) {
	r := NewRunner()
	dir := initRepoWithRemote(t)

	// No gone branches on a fresh repo.
	branches, err := r.GoneBranches(dir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}
	// We just need it to not error; could be empty or have entries.
	_ = branches
}

func TestGoneBranchesWithGone(t *testing.T) {
	r := NewRunner()
	// Create a server (bare) + client to simulate a gone upstream.
	serverDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main", serverDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v: %s", err, out)
	}

	clientDir := t.TempDir()
	cmd = exec.Command("git", "clone", serverDir, clientDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}

	for _, args := range [][]string{
		{"config", "user.email", "t@t.com"},
		{"config", "user.name", "T"},
	} {
		c := exec.Command("git", args...)
		c.Dir = clientDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Push initial commit.
	readme := filepath.Join(clientDir, "README.md")
	if err := os.WriteFile(readme, []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
		{"push", "origin", "main"},
		// Create and push a feature branch.
		{"checkout", "-b", "gone-branch"},
		{"push", "-u", "origin", "gone-branch"},
	} {
		c := exec.Command("git", args...)
		c.Dir = clientDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Delete the remote branch (simulating "gone").
	cmd = exec.Command("git", "push", "origin", "--delete", "gone-branch")
	cmd.Dir = clientDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("delete remote branch: %v: %s", err, out)
	}

	// Fetch with --prune so the tracking ref is removed.
	cmd = exec.Command("git", "fetch", "--prune", "origin")
	cmd.Dir = clientDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fetch prune: %v: %s", err, out)
	}

	branches, err := r.GoneBranches(clientDir)
	if err != nil {
		t.Fatalf("GoneBranches: %v", err)
	}

	found := false
	for _, b := range branches {
		if b == "gone-branch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected gone-branch in GoneBranches, got %v", branches)
	}
}

func TestGoneBranchesError(t *testing.T) {
	r := NewRunner()
	dir := t.TempDir() // not a git repo
	_, err := r.GoneBranches(dir)
	if err == nil {
		t.Error("expected error from GoneBranches on non-git dir")
	}
}

func TestMergedBranchesError(t *testing.T) {
	r := NewRunner()
	dir := t.TempDir() // not a git repo
	_, err := r.MergedBranches(dir, "main")
	if err == nil {
		t.Error("expected error from MergedBranches on non-git dir")
	}
}

func TestClone(t *testing.T) {
	r := NewRunner()
	// Use a local bare repo as the clone source.
	sourceDir := initRepo(t)
	parentDir := t.TempDir()

	if err := r.Clone(parentDir, sourceDir, "cloned-repo"); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	clonedDir := filepath.Join(parentDir, "cloned-repo")
	if !r.IsGitRepo(clonedDir) {
		t.Error("expected cloned dir to be a git repo")
	}
}

func TestCloneError(t *testing.T) {
	r := NewRunner()
	parentDir := t.TempDir()
	if err := r.Clone(parentDir, "/no/such/path", "bad-repo"); err == nil {
		t.Error("expected error cloning from nonexistent source")
	}
}

func TestRemoteURLGitHub(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add remote: %v: %s", err, out)
	}

	url, err := r.RemoteURL(dir)
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if url != "https://github.com/user/repo.git" {
		t.Errorf("RemoteURL = %q, want https://github.com/user/repo.git", url)
	}
}

func TestRemoteURLError(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t) // no remote
	_, err := r.RemoteURL(dir)
	if err == nil {
		t.Error("expected error from RemoteURL with no remote")
	}
}

func TestStatusDirtyError(t *testing.T) {
	r := NewRunner()
	dir := t.TempDir() // not a git repo
	_, err := r.StatusDirty(dir)
	if err == nil {
		t.Error("expected error from StatusDirty on non-git dir")
	}
}

func TestCheckoutBranch(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)

	// Create a second branch to switch to.
	cmd := exec.Command("git", "branch", "other")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch other: %v: %s", err, out)
	}

	if err := r.CheckoutBranch(dir, "other"); err != nil {
		t.Fatalf("CheckoutBranch: %v", err)
	}

	branch, err := r.CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch after checkout: %v", err)
	}
	if branch != "other" {
		t.Errorf("branch = %q, want other after checkout", branch)
	}
}

func TestCheckoutBranchError(t *testing.T) {
	r := NewRunner()
	dir := initRepo(t)
	if err := r.CheckoutBranch(dir, "no-such-branch"); err == nil {
		t.Error("expected error checking out non-existent branch")
	}
}
