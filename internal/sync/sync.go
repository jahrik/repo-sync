package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
	githubclient "github.com/jahrik/repo-sync/internal/github"

	gogithub "github.com/google/go-github/v72/github"
)

// Run is the top-level sync orchestration.  It lists repositories, clones
// missing ones, then fan-outs workers to sync each existing directory.
func Run(
	ctx context.Context,
	cfg config.Config,
	gh githubclient.Client,
	gitRunner git.Runner,
	baseDir string,
) ([]RepoResult, error) {
	repos, err := gh.ListRepos(ctx, cfg.Limit)
	if err != nil {
		return nil, fmt.Errorf("sync: list repos: %w", err)
	}

	// Build a map of name → repo for quick lookup.
	repoMap := make(map[string]*gogithub.Repository, len(repos))
	for _, r := range repos {
		repoMap[r.GetName()] = r
	}

	// Phase 1: clone missing repositories.
	var results []RepoResult
	var existingDirs []string

	for _, r := range repos {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		name := r.GetName()
		if err := safeRepoName(name); err != nil {
			results = append(results, RepoResult{
				Name:   name,
				Status: StatusError,
				Err:    fmt.Errorf("skipping repo with unsafe name: %w", err),
			})
			continue
		}
		dir := filepath.Join(baseDir, name)

		if _, err := os.Stat(dir); os.IsNotExist(err) {
			cloneURL := cloneURLFor(r, cfg.UseSSH)
			cloneErr := gitRunner.Clone(baseDir, cloneURL, name)
			status := StatusCloned
			var cloneErrOut error
			if cloneErr != nil {
				status = StatusError
				cloneErrOut = cloneErr
			}
			results = append(results, RepoResult{
				Name:   name,
				Status: status,
				Err:    cloneErrOut,
			})
		} else {
			existingDirs = append(existingDirs, dir)
		}
	}

	if len(existingDirs) == 0 {
		return results, nil
	}

	// Phase 2: worker pool for existing directories.
	workers := len(existingDirs)
	if max := runtime.NumCPU() * 4; workers > max {
		workers = max
	}

	type job struct {
		dir  string
		repo *gogithub.Repository
	}

	jobs := make(chan job, len(existingDirs))
	out := make(chan RepoResult, len(existingDirs))

	for i := 0; i < workers; i++ {
		go func() {
			for j := range jobs {
				out <- syncOne(ctx, cfg, gh, gitRunner, j.dir, j.repo)
			}
		}()
	}

	for _, dir := range existingDirs {
		name := filepath.Base(dir)
		jobs <- job{dir: dir, repo: repoMap[name]}
	}
	close(jobs)

	for range existingDirs {
		results = append(results, <-out)
	}

	return results, nil
}

// syncOne processes a single repository directory.
func syncOne(
	ctx context.Context,
	cfg config.Config,
	gh githubclient.Client,
	gitRunner git.Runner,
	dir string,
	repo *gogithub.Repository,
) (result RepoResult) {
	name := filepath.Base(dir)
	result = RepoResult{Name: name, Status: StatusError}

	// Recover from panics in worker goroutines.
	defer func() {
		if r := recover(); r != nil {
			result.Err = fmt.Errorf("panic in syncOne for %s: %v", name, r)
			result.Status = StatusError
		}
	}()

	if !gitRunner.IsGitRepo(dir) {
		result.Err = fmt.Errorf("%s: directory exists but is not a git repo", name)
		return result
	}

	// Check if this is a github.com repo; skip non-github remotes.
	_, remoteErr := gitRunner.RemoteURL(dir)
	if errors.Is(remoteErr, git.ErrNotGitHub) {
		result.Status = StatusOK
		result.Err = nil
		return result
	}

	if err := gitRunner.FetchPrune(dir); err != nil {
		result.Err = err
		return result
	}

	defaultBranch, err := gitRunner.DefaultBranch(dir)
	if err != nil {
		result.Err = err
		return result
	}
	if repo != nil && repo.GetDefaultBranch() != "" {
		defaultBranch = repo.GetDefaultBranch()
	}

	currentBranch, err := gitRunner.CurrentBranch(dir)
	if err != nil {
		result.Err = err
		return result
	}

	isOnDefault := currentBranch == defaultBranch

	var wasBehind bool
	var ahead, behind int

	if isOnDefault {
		a, b, err := gitRunner.AheadBehind(dir, currentBranch, defaultBranch)
		if err == nil {
			ahead = a
			behind = b
		}
		if behind > 0 {
			wasBehind = true
			if pullErr := gitRunner.PullFFOnly(dir); pullErr != nil {
				// Can't fast-forward; report dirty/diverged.
				result.Status = StatusDirty
				result.Err = pullErr
				result.Branch = currentBranch
				return result
			}
		}
		_ = ahead
	} else {
		a, b, err := gitRunner.AheadBehind(dir, currentBranch, defaultBranch)
		if err == nil {
			ahead = a
			behind = b
		}
		_ = b
	}
	_ = behind

	isDirty, err := gitRunner.StatusDirty(dir)
	if err != nil {
		isDirty = false // best-effort
	}

	var openPRs, mergedPRs []*gogithub.PullRequest
	if !isOnDefault && gh != nil {
		owner, repoName, parseErr := parseOwnerRepoFromDir(gitRunner, dir)
		if parseErr == nil {
			openPRs, _ = gh.ListOpenPRs(ctx, owner, repoName, currentBranch)
			mergedPRs, _ = gh.ListMergedPRs(ctx, owner, repoName, currentBranch)
		}
	}

	in := DecisionInput{
		CurrentBranch: currentBranch,
		DefaultBranch: defaultBranch,
		Ahead:         ahead,
		Behind:        behind,
		OpenPRs:       openPRs,
		MergedPRs:     mergedPRs,
		IsDirty:       isDirty,
		IsOnDefault:   isOnDefault,
		WasBehind:     wasBehind,
	}

	decided := Decide(in)
	decided.Name = name

	// CLEANED: switch to default branch and pull. Branches are not deleted —
	// the tool is intentionally non-destructive on the local filesystem.
	if decided.Status == StatusCleaned {
		_ = gitRunner.Checkout(dir, defaultBranch)
		_ = gitRunner.PullFFOnly(dir)
	}

	return decided
}

// parseOwnerRepoFromDir retrieves the remote URL and parses owner/repo.
func parseOwnerRepoFromDir(gitRunner git.Runner, dir string) (owner, repo string, err error) {
	url, err := gitRunner.RemoteURL(dir)
	if err != nil {
		return "", "", err
	}
	return parseOwnerRepo(url)
}

// cloneURLFor returns the appropriate clone URL for a repository.
func cloneURLFor(r *gogithub.Repository, useSSH bool) string {
	if useSSH {
		return r.GetSSHURL()
	}
	return r.GetCloneURL()
}

// safeRepoName validates that a repository name from the GitHub API is safe to
// use as a filesystem directory name under baseDir.  It rejects names that
// contain path separators (which would escape the base directory after
// filepath.Join) and names that start with "-" (which git could misinterpret
// as flags even when "--" is used by older git versions in some code paths).
func safeRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("repo name is empty")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf("repo name %q contains a path separator", name)
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("repo name %q starts with '-'", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("repo name %q is a reserved path component", name)
	}
	return nil
}
