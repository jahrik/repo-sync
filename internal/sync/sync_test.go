package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	gogithub "github.com/google/go-github/v72/github"
	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
)

// fakeGitRunner implements git.Runner for tests.
type fakeGitRunner struct {
	mu               sync.Mutex
	isGitRepo        bool
	fetchErr         error
	defaultBranch    string
	currentBranch    string
	remoteURL        string
	remoteURLErr     error
	ahead            int
	behind           int
	isDirty          bool
	cloneErr         error
	clonedNames      []string
	checkedOutBranch string
	pullFFOnlyCalls  int
	pullFFOnlyErr    error
	checkoutErr      error
}

func (f *fakeGitRunner) IsGitRepo(_ string) bool   { return f.isGitRepo }
func (f *fakeGitRunner) FetchPrune(_ string) error { return f.fetchErr }
func (f *fakeGitRunner) DefaultBranch(_ string) (string, error) {
	return f.defaultBranch, nil
}
func (f *fakeGitRunner) CurrentBranch(_ string) (string, error) {
	return f.currentBranch, nil
}
func (f *fakeGitRunner) RemoteURL(_ string) (string, error) {
	return f.remoteURL, f.remoteURLErr
}
func (f *fakeGitRunner) AheadBehind(_, _, _ string) (int, int, error) {
	return f.ahead, f.behind, nil
}
func (f *fakeGitRunner) PullFFOnly(_ string) error {
	f.mu.Lock()
	f.pullFFOnlyCalls++
	f.mu.Unlock()
	return f.pullFFOnlyErr
}
func (f *fakeGitRunner) CheckoutBranch(_, branch string) error {
	f.mu.Lock()
	f.checkedOutBranch = branch
	f.currentBranch = branch
	f.mu.Unlock()
	return f.checkoutErr
}
func (f *fakeGitRunner) MergedBranches(_, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeGitRunner) GoneBranches(_ string) ([]string, error) { return nil, nil }
func (f *fakeGitRunner) StatusDirty(_ string) (bool, error) {
	return f.isDirty, nil
}
func (f *fakeGitRunner) Clone(_, _, name string) error {
	f.mu.Lock()
	f.clonedNames = append(f.clonedNames, name)
	f.mu.Unlock()
	return f.cloneErr
}

func TestFakeGitRunnerCheckoutUpdatesCurrentBranch(t *testing.T) {
	r := &fakeGitRunner{currentBranch: "feature"}
	if err := r.CheckoutBranch("", "main"); err != nil {
		t.Fatalf("CheckoutBranch: %v", err)
	}
	branch, err := r.CurrentBranch("")
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("CurrentBranch = %q, want main", branch)
	}
}

// Compile-time interface check.
var _ git.Runner = (*fakeGitRunner)(nil)

// fakeGHClient satisfies the github.Client interface with empty responses.
type fakeGHClient struct {
	repos []*gogithub.Repository
}

func (f *fakeGHClient) Owner() string { return "" }
func (f *fakeGHClient) ListRepos(_ context.Context, _ int) ([]*gogithub.Repository, error) {
	return f.repos, nil
}
func (f *fakeGHClient) ListOpenPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}
func (f *fakeGHClient) ListMergedPRs(_ context.Context, _, _, _ string) ([]*gogithub.PullRequest, error) {
	return nil, nil
}

func strPtrS(s string) *string { return &s }

func TestRunClonesNewRepo(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "new-repo"

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/new-repo.git"),
				SSHURL:        strPtrS("git@github.com:test/new-repo.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusCloned {
		t.Errorf("status = %q, want CLONED", results[0].Status)
	}
	if len(gitRunner.clonedNames) != 1 || gitRunner.clonedNames[0] != repoName {
		t.Errorf("clonedNames = %v, want [%s]", gitRunner.clonedNames, repoName)
	}
}

func TestRunReportOrphans(t *testing.T) {
	baseDir := t.TempDir()

	// Create a local dir that won't appear in the API response.
	orphanDir := filepath.Join(baseDir, "orphan-repo")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS("known-repo"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/known-repo.git"), SSHURL: strPtrS("git@github.com:t/known-repo.git")},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, ReportOrphans: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var orphaned []RepoResult
	for _, r := range results {
		if r.Status == StatusOrphaned {
			orphaned = append(orphaned, r)
		}
	}
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned result, got %d", len(orphaned))
	}
	if orphaned[0].Name != "orphan-repo" {
		t.Errorf("orphaned name = %q, want orphan-repo", orphaned[0].Name)
	}
}

func TestRunIgnoreListSuppressesOrphans(t *testing.T) {
	baseDir := t.TempDir()

	for _, name := range []string{"orphan-repo", ".claude", "scripts"} {
		if err := os.MkdirAll(filepath.Join(baseDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS("known-repo"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/known-repo.git"), SSHURL: strPtrS("git@github.com:t/known-repo.git")},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, ReportOrphans: true, Ignore: []string{".claude", "scripts"}}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var orphaned []RepoResult
	for _, r := range results {
		if r.Status == StatusOrphaned {
			orphaned = append(orphaned, r)
		}
	}
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned result, got %d: %v", len(orphaned), orphaned)
	}
	if orphaned[0].Name != "orphan-repo" {
		t.Errorf("orphaned name = %q, want orphan-repo", orphaned[0].Name)
	}
}

func TestRunClonesNewRepoSSH(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "new-repo-ssh"

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/new-repo-ssh.git"),
				SSHURL:        strPtrS("git@github.com:test/new-repo-ssh.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, UseSSH: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusCloned {
		t.Errorf("status = %q, want CLONED", results[0].Status)
	}
}

func TestRunSyncsExistingRepoOK(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "existing-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/existing-repo.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "main",
		remoteURL:     "https://github.com/test/existing-repo.git",
		isDirty:       false,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusOK {
		t.Errorf("status = %q, want OK", results[0].Status)
	}
}

func TestRunFetchReportsBehindWithoutPulling(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "fetch-behind"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS(repoName), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/t/fetch-behind.git")},
		},
	}

	pulled := false
	gitRunner := &fakePullTracker{
		fakeGitRunner: fakeGitRunner{
			isGitRepo:     true,
			defaultBranch: "main",
			currentBranch: "main",
			remoteURL:     "https://github.com/t/fetch-behind.git",
			behind:        2,
		},
		onPull: func() { pulled = true },
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Fetch: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusBehind {
		t.Errorf("--fetch: status = %q, want BEHIND", results[0].Status)
	}
	if pulled {
		t.Error("--fetch: PullFFOnly should NOT be called")
	}
}

type fakePullTracker struct {
	fakeGitRunner
	onPull func()
}

func (f *fakePullTracker) PullFFOnly(_ string) error {
	if f.onPull != nil {
		f.onPull()
	}
	return nil
}

func TestRunExistingRepoOKWithoutPull(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "existing-no-pull"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/existing-no-pull.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false} // should never be called
	cfg := config.Config{Dir: baseDir, Limit: 10} // no Pull flag
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusOK {
		t.Errorf("clone-only mode: status = %q, want OK for existing repo", results[0].Status)
	}
}

func TestRunSyncsExistingRepoDirty(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "dirty-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/dirty-repo.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:     true,
		defaultBranch: "main",
		currentBranch: "main",
		remoteURL:     "https://github.com/test/dirty-repo.git",
		isDirty:       true,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusDirty {
		t.Errorf("status = %q, want DIRTY", results[0].Status)
	}
}

func TestRunSkipsNonGitHubRemote(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "non-github-repo"
	repoDir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/non-github-repo.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo:    true,
		remoteURL:    "https://gitlab.com/foo/bar.git",
		remoteURLErr: git.ErrNotGitHub,
	}

	cfg := config.Config{Dir: baseDir, Limit: 10, Pull: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusOK {
		t.Errorf("non-github repo should report OK, got %q", results[0].Status)
	}
}

func TestRunHandlesCloneError(t *testing.T) {
	baseDir := t.TempDir()
	repoName := "fail-repo"

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS(repoName),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/test/fail-repo.git"),
			},
		},
	}

	gitRunner := &fakeGitRunner{
		isGitRepo: false,
		cloneErr:  errors.New("network error"),
	}

	cfg := config.Config{Dir: baseDir, Limit: 10}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusError {
		t.Errorf("status = %q, want ERROR", results[0].Status)
	}
	if results[0].Err == nil {
		t.Error("expected non-nil Err for clone failure")
	}
}

func boolPtrS(b bool) *bool { return &b }

func TestRunSkipForks(t *testing.T) {
	baseDir := t.TempDir()

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS("original"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/u/original.git"), SSHURL: strPtrS("git@github.com:u/original.git"), Fork: boolPtrS(false)},
			{Name: strPtrS("forked"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/u/forked.git"), SSHURL: strPtrS("git@github.com:u/forked.git"), Fork: boolPtrS(true)},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, SkipForks: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after skip-forks, got %d", len(results))
	}
	if results[0].Name != "original" {
		t.Errorf("expected original, got %s", results[0].Name)
	}
}

func TestRunSkipArchived(t *testing.T) {
	baseDir := t.TempDir()

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{Name: strPtrS("active"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/u/active.git"), SSHURL: strPtrS("git@github.com:u/active.git"), Archived: boolPtrS(false)},
			{Name: strPtrS("archived"), DefaultBranch: strPtrS("main"), CloneURL: strPtrS("https://github.com/u/archived.git"), SSHURL: strPtrS("git@github.com:u/archived.git"), Archived: boolPtrS(true)},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, SkipArchived: true}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after skip-archived, got %d", len(results))
	}
	if results[0].Name != "active" {
		t.Errorf("expected active, got %s", results[0].Name)
	}
}

func TestRunOwnerFilter(t *testing.T) {
	baseDir := t.TempDir()

	ownerPtr := func(s string) *gogithub.User { u := &gogithub.User{}; login := s; u.Login = &login; return u }

	gh := &fakeGHClient{
		repos: []*gogithub.Repository{
			{
				Name:          strPtrS("my-repo"),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/alice/my-repo.git"),
				SSHURL:        strPtrS("git@github.com:alice/my-repo.git"),
				Owner:         ownerPtr("alice"),
			},
			{
				Name:          strPtrS("org-repo"),
				DefaultBranch: strPtrS("main"),
				CloneURL:      strPtrS("https://github.com/SomeOrg/org-repo.git"),
				SSHURL:        strPtrS("git@github.com:SomeOrg/org-repo.git"),
				Owner:         ownerPtr("SomeOrg"),
			},
		},
	}

	gitRunner := &fakeGitRunner{isGitRepo: false}
	cfg := config.Config{Dir: baseDir, Limit: 10, Owner: "alice"}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after owner filter, got %d", len(results))
	}
	if results[0].Name != "my-repo" {
		t.Errorf("expected my-repo, got %s", results[0].Name)
	}
}
