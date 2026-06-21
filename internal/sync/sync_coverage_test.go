package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogithub "github.com/google/go-github/v72/github"
	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
)

// errRemote is a sentinel error for remote URL failures.
var errRemote = errors.New("simulated remote URL error")

// fakeGitRunnerRemoteErr is a fakeGitRunner that returns an error from RemoteURL.
type fakeGitRunnerRemoteErr struct {
	fakeGitRunner
}

func (f *fakeGitRunnerRemoteErr) RemoteURL(_ string) (string, error) {
	return "", errRemote
}

// TestParseOwnerRepoFromDirRemoteURLError exercises the error branch in
// parseOwnerRepoFromDir where gitRunner.RemoteURL returns an error.
func TestParseOwnerRepoFromDirRemoteURLError(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeGitRunnerRemoteErr{}
	_, _, err := parseOwnerRepoFromDir(runner, dir)
	if err == nil {
		t.Error("expected error when RemoteURL fails")
	}
	if !errors.Is(err, errRemote) {
		t.Errorf("error = %v, want errRemote", err)
	}
}

// TestSyncOneDefaultBranchBehindPullSucceedsNotDirty exercises the path where
// the default branch was behind, pull succeeds, and status is not dirty.
func TestSyncOneDefaultBranchBehindPullSucceeds2(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "behind-ok-2"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/behind-ok-2.git")},
		},
	}

	// behind=1, pull succeeds → StatusPulled
	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "main",
		remoteURL:     "https://github.com/t/behind-ok-2.git",
		behind:        1,
		isDirty:       false,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusPulled {
		t.Errorf("status = %q, want PULLED", results[0].Status)
	}
}

// TestSyncOneCurrentBranchErrorPath exercises the error from CurrentBranch.
func TestSyncOneCurrentBranchError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "curr-branch-err"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/curr-err.git")},
		},
	}

	gitRunner := &fakeGitRunnerCurrBranchErr{
		fakeGitRunner: fakeGitRunner{
			isGitRepo:     true,
			defaultBranch: "main",
			remoteURL:     "https://github.com/t/curr-err.git",
		},
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusError {
		t.Errorf("status = %q, want ERROR when CurrentBranch fails", results[0].Status)
	}
}

type fakeGitRunnerCurrBranchErr struct {
	fakeGitRunner
}

func (f *fakeGitRunnerCurrBranchErr) CurrentBranch(_ string) (string, error) {
	return "", errors.New("simulated CurrentBranch error")
}

// TestSyncOneDefaultBranchErrorPath exercises the error from DefaultBranch.
func TestSyncOneDefaultBranchError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "default-branch-err"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS(""), CloneURL: strPtrS("https://github.com/t/default-err.git")},
		},
	}

	gitRunner := &fakeGitRunnerDefaultBranchErr{
		fakeGitRunner: fakeGitRunner{
			isGitRepo: true,
			remoteURL: "https://github.com/t/default-err.git",
		},
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusError {
		t.Errorf("status = %q, want ERROR when DefaultBranch fails", results[0].Status)
	}
}

type fakeGitRunnerDefaultBranchErr struct {
	fakeGitRunner
}

func (f *fakeGitRunnerDefaultBranchErr) DefaultBranch(_ string) (string, error) {
	return "", errors.New("simulated DefaultBranch error")
}

// TestSyncOneNilGHClientSkipsPRLookup verifies that when gh is nil and the
// repo is on a non-default branch, the PR lookup is skipped gracefully.
func TestSyncOneNilGHClientSkipsPRLookup(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "nil-gh-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// We need a run with an existing dir and nil gh — but Run requires a
	// non-nil gh for ListRepos.  Instead call syncOne directly.
	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/x",
		remoteURL:     "https://github.com/owner/nil-gh-repo.git",
		ahead:         2,
	}

	cfg := config.Config{Limit: 10}
	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, nil)
	// With nil gh and ahead>0, no PR lookup → UNMERGED.
	if result.Status != StatusUnmerged {
		t.Errorf("status = %q, want UNMERGED when gh is nil", result.Status)
	}
}

// TestSyncOneRepoProvidesDefaultBranch exercises the code path where the
// repo parameter's DefaultBranch overrides the git-detected default.
func TestSyncOneRepoBranchOverride(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "branch-override"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Git reports "master" but the GitHub repo object says "main".
	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "master",
		currentBranch: "main",
		remoteURL:     "https://github.com/owner/branch-override.git",
	}

	repo := &gogithub.Repository{
		Name:          strPtrS(repoName),
		DefaultBranch: strPtrS("main"),
	}

	cfg := config.Config{Limit: 10}
	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, repo)
	// currentBranch=="main" == defaultBranch from repo → isOnDefault=true → OK
	if result.Status != StatusOK {
		t.Errorf("status = %q, want OK when repo overrides default branch", result.Status)
	}
}

// TestRunListReposError exercises the error path when ListRepos fails.
func TestRunListReposError(t *testing.T) {
	baseDir := t.TempDir()

	gh := &fakeGHClientError{}
	gitRunner := &fakeGitRunner{}
	cfg := config.Config{Dir: baseDir, Limit: 10}

	_, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err == nil {
		t.Error("expected error when ListRepos fails")
	}
}

type fakeGHClientError struct{}

func (f *fakeGHClientError) Owner() string { return "" }
func (f *fakeGHClientError) ListRepos(_ context.Context, _ int) ([]*gogithub.Repository, error) {
	return nil, errors.New("simulated ListRepos error")
}
func (f *fakeGHClientError) ListOpenPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}
func (f *fakeGHClientError) ListMergedPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}

// TestDecideOnDefaultDirtyAndWasBehind exercises the IsOnDefault=true branch
// when IsDirty=true AND WasBehind=true simultaneously.  Dirty takes priority.
func TestDecideOnDefaultDirtyAndWasBehind(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "main",
		DefaultBranch: "main",
		IsOnDefault:   true,
		IsDirty:       true,
		WasBehind:     true,
	}
	got := Decide(in)
	if got.Status != StatusDirty {
		t.Errorf("status = %q, want DIRTY (dirty takes priority over behind)", got.Status)
	}
}

// TestSyncOneRemoteURLErrorIsNotErrNotGitHub exercises the path where
// RemoteURL returns a non-ErrNotGitHub error (e.g. no remote configured).
func TestSyncOneRemoteURLNonNotGitHubError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "remote-err-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// RemoteURL returns a non-nil, non-ErrNotGitHub error.
	gitRunner := &fakeGitRunnerRemoteErrFetch{
		fakeGitRunner: fakeGitRunner{
			isGitRepo: true,
		},
	}

	cfg := config.Config{Limit: 10}
	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, nil)
	// remoteURLErr is not ErrNotGitHub, so we proceed to FetchPrune.
	// FetchPrune also fails → StatusError.
	if result.Status != StatusError {
		t.Errorf("status = %q, want ERROR when RemoteURL fails with non-ErrNotGitHub", result.Status)
	}
}

// fakeGitRunnerRemoteErrFetch has a non-ErrNotGitHub remote URL error and
// a fetch error so syncOne returns ERROR.
type fakeGitRunnerRemoteErrFetch struct {
	fakeGitRunner
}

func (f *fakeGitRunnerRemoteErrFetch) RemoteURL(_ string) (string, error) {
	return "", errors.New("no remote configured")
}

func (f *fakeGitRunnerRemoteErrFetch) FetchPrune(_ string) error {
	return errors.New("no remote configured")
}

// TestSyncOneFeatureBranchWithMergedPR exercises the non-default branch path
// where there are merged PRs → SYNCED.
func TestSyncOneFeatureBranchWithMergedPR(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "merged-pr-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	now := gogithub.Timestamp{}
	_ = now
	mergedPRs := []*gogithub.PullRequest{
		{Number: intPtrS(5), Title: strPtrS("done")},
	}
	ghWithMergedPR := &fakeGHClientWithMergedPRs{
		repos:     []*gogithub.Repository{{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/owner/merged-pr-repo.git")}},
		mergedPRs: mergedPRs,
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/done",
		remoteURL:     "https://github.com/owner/merged-pr-repo.git",
		ahead:         3,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, ghWithMergedPR, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusSynced {
		t.Errorf("status = %q, want SYNCED when merged PR exists", results[0].Status)
	}
}

type fakeGHClientWithMergedPRs struct {
	repos     []*gogithub.Repository
	mergedPRs []*gogithub.PullRequest
}

func (f *fakeGHClientWithMergedPRs) Owner() string { return "" }
func (f *fakeGHClientWithMergedPRs) ListRepos(_ context.Context, _ int) ([]*gogithub.Repository, error) {
	return f.repos, nil
}
func (f *fakeGHClientWithMergedPRs) ListOpenPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}
func (f *fakeGHClientWithMergedPRs) ListMergedPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return f.mergedPRs, nil
}

// TestParseOwnerRepoSSHInvalidPath exercises parseOwnerRepo with an SSH URL
// that has an empty owner or repo component.
func TestParseOwnerRepoSSHInvalidPath(t *testing.T) {
	tests := []struct {
		url string
	}{
		{"git@github.com:/repo.git"},  // empty owner
		{"git@github.com:owner/.git"}, // empty repo after trimming .git
		{"git@github.com:owner/"},     // empty repo
	}
	for _, tc := range tests {
		_, _, err := parseOwnerRepo(tc.url)
		if err == nil {
			t.Errorf("parseOwnerRepo(%q) expected error for invalid SSH URL", tc.url)
		}
	}
}

// TestParseOwnerRepoHTTPSInvalidPath exercises parseOwnerRepo with HTTPS URLs
// that produce invalid paths.
func TestParseOwnerRepoHTTPSEmptyComponents(t *testing.T) {
	tests := []struct {
		url string
	}{
		{"https://github.com/"},       // no owner or repo
		{"https://github.com/owner"},  // no repo
		{"https://github.com/owner/"}, // empty repo
	}
	for _, tc := range tests {
		_, _, err := parseOwnerRepo(tc.url)
		if err == nil {
			t.Errorf("parseOwnerRepo(%q) expected error for invalid HTTPS URL", tc.url)
		}
	}
}

// TestSyncOneFeatureBranchAheadNoPR covers feature branch + ahead>0 + no PRs → UNMERGED.
func TestSyncOneFeatureBranchAhead(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "feat-ahead"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/feat-ahead.git")},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "feature/wip",
		remoteURL:     "https://github.com/t/feat-ahead.git",
		ahead:         5,
		behind:        1,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusUnmerged {
		t.Errorf("status = %q, want UNMERGED", results[0].Status)
	}
	if results[0].Ahead != 5 {
		t.Errorf("Ahead = %d, want 5", results[0].Ahead)
	}
}

// TestSyncOnePanicRecovery exercises the defer-recover panic handler in syncOne.
func TestSyncOnePanicRecovery(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "panic-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gitRunner := &fakeGitRunnerPanic{}
	cfg := config.Config{Limit: 10}

	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, nil)
	if result.Status != StatusError {
		t.Errorf("status = %q, want ERROR after panic recovery", result.Status)
	}
	if result.Err == nil {
		t.Error("expected non-nil Err after panic recovery")
	}
}

type fakeGitRunnerPanic struct {
	fakeGitRunner
}

func (f *fakeGitRunnerPanic) IsGitRepo(_ string) bool {
	panic("deliberate panic for testing")
}

// Ensure fakeGitRunnerPanic satisfies git.Runner interface.
var _ git.Runner = (*fakeGitRunnerPanic)(nil)

// TestSyncOneCheckoutSwitchesAndPulls verifies that --checkout switches a
// SYNCED repo to the default branch and fast-forward pulls it.
func TestSyncOneCheckoutSwitchesAndPulls(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "checkout-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "update-role",
		remoteURL:     "https://github.com/owner/checkout-repo.git",
		ahead:         0,
	}

	cfg := config.Config{Limit: 10, Pull: true, Checkout: true}
	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, nil)
	if result.Status != StatusSynced {
		t.Errorf("status = %q, want SYNCED", result.Status)
	}
}

// TestSyncOneCheckoutSkipsDirty verifies that --checkout does not switch a
// dirty repo (it should remain DIRTY, not attempt a checkout).
func TestSyncOneCheckoutSkipsDirty(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "dirty-checkout-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "update-role",
		remoteURL:     "https://github.com/owner/dirty-checkout-repo.git",
		ahead:         0,
		isDirty:       true,
	}

	cfg := config.Config{Limit: 10, Pull: true, Checkout: true}
	result := syncOne(context.Background(), cfg, nil, gitRunner, repoDir, nil)
	if result.Status != StatusDirty {
		t.Errorf("status = %q, want DIRTY (checkout must not touch dirty repos)", result.Status)
	}
}
