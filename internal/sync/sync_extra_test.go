package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogithub "github.com/google/go-github/v88/github"
	"github.com/jahrik/repo-sync/internal/config"
)

// fakeGitRunnerExt extends fakeGitRunner with configurable error returns for
// functions not covered in sync_test.go.
type fakeGitRunnerExt struct {
	fakeGitRunner
	aheadBehindErr error
	pullErr        error
	statusDirtyErr error
}

func (f *fakeGitRunnerExt) AheadBehind(_, _, _ string) (int, int, error) {
	return f.ahead, f.behind, f.aheadBehindErr
}

func (f *fakeGitRunnerExt) PullFFOnly(_ string) error {
	return f.pullErr
}

func (f *fakeGitRunnerExt) StatusDirty(_ string) (bool, error) {
	return f.isDirty, f.statusDirtyErr
}

// TestRunContextCancelled checks that a cancelled context is propagated.
func TestRunContextCancelled(t *testing.T) {
	baseDir := t.TempDir()

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS("repo-a"),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/repo-a.git"),
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10}
	_, err := Run(ctx, cfg, gh, gitRunner, baseDir, nil, nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// TestRunNoExistingDirs returns results with only CLONED entries when no dirs exist.
func TestRunAllCloned(t *testing.T) {
	baseDir := t.TempDir()

	repos := []*gogithub.Repository{
		{Name: strPtrS("r1"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/r1.git"), SSHURL: strPtrS("git@github.com:t/r1.git")},
		{Name: strPtrS("r2"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/r2.git"), SSHURL: strPtrS("git@github.com:t/r2.git")},
	}
	gh := &fakeGHClient{repos: repos}
	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10}

	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != StatusCloned {
			t.Errorf("repo %s: status = %q, want CLONED", r.Name, r.Status)
		}
	}
}

// TestSyncOneNotGitRepo covers the !IsGitRepo error path.
func TestSyncOneNotGitRepo(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "not-a-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/not-a-repo.git")},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}

	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusError {
		t.Errorf("status = %q, want ERROR", results[0].Status)
	}
}

// TestSyncOneFetchError covers the FetchPrune error path.
func TestSyncOneFetchError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "fetch-fail"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/fetch-fail.git")},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		remoteURL:     "https://github.com/t/fetch-fail.git",
		fetchErr:      errFetch,
		defaultBranch: "main",
		currentBranch: "main",
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusError {
		t.Errorf("status = %q, want ERROR", results[0].Status)
	}
}

// errFetch is a sentinel for fetch errors.
var errFetch = &fetchError{}

type fetchError struct{}

func (e *fetchError) Error() string { return "simulated fetch error" }

// TestSyncOneFeatureBranchWithOpenPR exercises the non-default branch + open PR code path.
func TestSyncOneFeatureBranchWithOpenPR(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "pr-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	pr := &gogithub.PullRequest{Number: intPtrS(42), Title: strPtrS("Great feature")}
	ghWithPR := &fakeGHClientWithPRs{
		repos:   []*gogithub.Repository{{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/owner/pr-repo.git")}},
		openPRs: []*gogithub.PullRequest{pr},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/great",
		remoteURL:     "https://github.com/owner/pr-repo.git",
		ahead:         2,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, ghWithPR, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusOpenPR {
		t.Errorf("status = %q, want OPEN PR", results[0].Status)
	}
	if results[0].PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", results[0].PRNumber)
	}
}

// TestSyncOneFeatureBranchBehindPull verifies pull is called when behind on default branch.
func TestSyncOneDefaultBranchBehindPullFails(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "behind-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/behind-repo.git")},
		},
	}

	gitRunner := &fakeGitRunnerExt{
		fakeGitRunner: fakeGitRunner{
			isGitRepo:     true,
			defaultBranch: "main",
			currentBranch: "main",
			remoteURL:     "https://github.com/t/behind-repo.git",
			behind:        3,
		},
		pullErr: &pullError{},
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusDirty {
		t.Errorf("status = %q, want DIRTY when pull fails", results[0].Status)
	}
}

type pullError struct{}

func (e *pullError) Error() string { return "pull --ff-only failed" }

// TestSyncOneDefaultBranchBehindPullSucceeds verifies PULLED status on successful pull.
func TestSyncOneDefaultBranchBehindPullSucceeds(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "behind-ok-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/behind-ok.git")},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "main",
		remoteURL:     "https://github.com/t/behind-ok.git",
		behind:        2,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusPulled {
		t.Errorf("status = %q, want PULLED when pull succeeds", results[0].Status)
	}
}

// TestSyncOneFeatureClean covers feature branch with no ahead commits and no PR → SYNCED.
func TestSyncOneFeatureClean(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "clean-feat-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/clean-feat.git")},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/done",
		remoteURL:     "https://github.com/t/clean-feat.git",
		ahead:         0,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusSynced {
		t.Errorf("status = %q, want SYNCED", results[0].Status)
	}
}

// TestSyncOneParseOwnerRepoFromDirError checks that a bad remote URL causes the
// owner/repo parse to fail gracefully (no panic, no PR lookup).
func TestSyncOneParseOwnerRepoFromDirError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "bad-url-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/bad-url.git")},
		},
	}

	// A github URL but malformed owner/repo path so parseOwnerRepo returns an error.
	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/something",
		remoteURL:     "https://github.com/single", // no owner/repo split
		ahead:         3,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// parse fails → no PR lookup → Ahead>0 no PR → UNMERGED
	if results[0].Status != StatusUnmerged {
		t.Errorf("status = %q, want UNMERGED when owner parse fails", results[0].Status)
	}
}

// TestCloneURLForSSH tests cloneURLFor with UseSSH=true.
func TestCloneURLForSSH(t *testing.T) {
	sshURL := "git@github.com:user/repo.git"
	httpsURL := "https://github.com/user/repo.git"
	r := &gogithub.Repository{
		SSHURL:   &sshURL,
		CloneURL: &httpsURL,
	}
	got := cloneURLFor(r, true)
	if got != sshURL {
		t.Errorf("cloneURLFor(useSSH=true) = %q, want %q", got, sshURL)
	}
	got = cloneURLFor(r, false)
	if got != httpsURL {
		t.Errorf("cloneURLFor(useSSH=false) = %q, want %q", got, httpsURL)
	}
}

// TestParseOwnerRepoFromDir exercises parseOwnerRepoFromDir via fakeGitRunner.
func TestParseOwnerRepoFromDir(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeGitRunner{remoteURL: "git@github.com:owner/myrepo.git"}
	owner, repo, err := parseOwnerRepoFromDir(runner, dir)
	if err != nil {
		t.Fatalf("parseOwnerRepoFromDir: %v", err)
	}
	if owner != "owner" || repo != "myrepo" {
		t.Errorf("owner=%q repo=%q, want owner/myrepo", owner, repo)
	}
}

// TestParseOwnerRepoFromDirError exercises parseOwnerRepoFromDir error path.
func TestParseOwnerRepoFromDirError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeGitRunner{remoteURL: "https://github.com/single"} // bad URL
	_, _, err := parseOwnerRepoFromDir(runner, dir)
	if err == nil {
		t.Error("expected error for bad remote URL")
	}
}

// fakeGHClientWithPRs is a fakeGHClient that also returns configured PRs.
type fakeGHClientWithPRs struct {
	repos   []*gogithub.Repository
	openPRs []*gogithub.PullRequest
}

func (f *fakeGHClientWithPRs) Owner() string { return "" }
func (f *fakeGHClientWithPRs) ListRepos(_ context.Context, _ int) ([]*gogithub.Repository, error) {
	return f.repos, nil
}
func (f *fakeGHClientWithPRs) ListOpenPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return f.openPRs, nil
}
func (f *fakeGHClientWithPRs) ListMergedPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}

func intPtrS(i int) *int { return &i }
