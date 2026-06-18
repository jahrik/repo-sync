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

// fakeGitRunner implements git.Runner for tests.
type fakeGitRunner struct {
	isGitRepo     bool
	fetchErr      error
	defaultBranch string
	currentBranch string
	remoteURL     string
	remoteURLErr  error
	ahead         int
	behind        int
	isDirty       bool
	cloneErr      error
	clonedNames   []string
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
func (f *fakeGitRunner) Checkout(_, _ string) error           { return nil }
func (f *fakeGitRunner) PullFFOnly(_ string) error            { return nil }
func (f *fakeGitRunner) DeleteLocalBranch(_, _ string) error  { return nil }
func (f *fakeGitRunner) DeleteRemoteBranch(_, _ string) error { return nil }
func (f *fakeGitRunner) MergedBranches(_, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeGitRunner) GoneBranches(_ string) ([]string, error) { return nil, nil }
func (f *fakeGitRunner) StatusDirty(_ string) (bool, error) {
	return f.isDirty, nil
}
func (f *fakeGitRunner) Clone(_, _, name string) error {
	f.clonedNames = append(f.clonedNames, name)
	return f.cloneErr
}

// Compile-time interface check.
var _ git.Runner = (*fakeGitRunner)(nil)

// fakeGHClient satisfies the github.Client interface with empty responses.
type fakeGHClient struct {
	repos []*gogithub.Repository
}

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
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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

	cfg := config.Config{Dir: baseDir, Limit: 10}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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

	cfg := config.Config{Dir: baseDir, Limit: 10}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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

	cfg := config.Config{Dir: baseDir, Limit: 10}
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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
	results, err := Run(context.Background(), cfg, gh, gitRunner, baseDir)
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
