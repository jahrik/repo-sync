package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
	githubclient "github.com/jahrik/repo-sync/internal/github"

	gogithub "github.com/google/go-github/v88/github"
)

// Run is the top-level sync orchestration.
//
// Default behaviour (no flags): lists repos, clones missing ones, marks
// existing dirs as OK without touching them.
//
// With cfg.Pull=true  : also fetch + fast-forward-pull existing repos and
//
//	report their branch status (dirty, behind, open PR …).
//
// With cfg.Fetch=true: fetch + prune existing repos and report status (no writes).
//
//	when the current branch has been merged or has no
//	open PR and no commits ahead.
//
// onStart is called once after the repo list is fetched, with
// (total, toClone, toSync) counts.  It may be nil.
//
// onResult is called after each repo is processed (from any goroutine).
// It may be nil.
func Run(
	ctx context.Context,
	cfg config.Config,
	gh githubclient.Client,
	gitRunner git.Runner,
	baseDir string,
	onStart func(total, clones, existing int),
	onResult func(result RepoResult),
) ([]RepoResult, error) {
	repos, err := gh.ListRepos(ctx, cfg.Limit)
	if err != nil {
		return nil, fmt.Errorf("sync: list repos: %w", err)
	}

	if cfg.Owner != "" {
		filtered := repos[:0]
		for _, r := range repos {
			if r.GetOwner().GetLogin() == cfg.Owner {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	if cfg.SkipForks {
		filtered := repos[:0]
		for _, r := range repos {
			if !r.GetFork() {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	if cfg.SkipArchived {
		filtered := repos[:0]
		for _, r := range repos {
			if !r.GetArchived() {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	if cfg.Filter != "" {
		re, err := regexp.Compile(cfg.Filter)
		if err != nil {
			return nil, fmt.Errorf("sync: invalid --filter regexp %q: %w", cfg.Filter, err)
		}
		filtered := repos[:0]
		for _, r := range repos {
			if re.MatchString(r.GetName()) {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	// Pre-scan: split into repos to clone vs existing dirs.
	var toClone []*gogithub.Repository
	var existingDirs []string
	for _, r := range repos {
		name := r.GetName()
		if safeRepoName(name) != nil {
			continue // will become an error result in Phase 1
		}
		dir := filepath.Join(baseDir, name)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			toClone = append(toClone, r)
		} else {
			existingDirs = append(existingDirs, dir)
		}
	}

	if onStart != nil {
		onStart(len(repos), len(toClone), len(existingDirs))
	}

	// Build a map of name → repo for Phase 2 lookup.
	repoMap := make(map[string]*gogithub.Repository, len(repos))
	for _, r := range repos {
		repoMap[r.GetName()] = r
	}

	// Phase 1: handle unsafe names synchronously, then clone missing repos concurrently.
	var results []RepoResult

	// Check for cancellation before any I/O.
	select {
	case <-ctx.Done():
		return results, ctx.Err()
	default:
	}

	// Report repos with unsafe names up front (sequential, no I/O).
	for _, r := range repos {
		name := r.GetName()
		if err := safeRepoName(name); err != nil {
			res := RepoResult{
				Name:   name,
				Status: StatusError,
				Err:    fmt.Errorf("skipping repo with unsafe name: %w", err),
			}
			results = append(results, res)
			if onResult != nil {
				onResult(res)
			}
		}
	}

	// Clone missing repos concurrently.
	cloneWorkers := len(toClone)
	if cloneWorkers == 0 {
		// nothing to clone
	} else {
		if max := runtime.NumCPU() * 4; cloneWorkers > max {
			cloneWorkers = max
		}

		type cloneJob struct{ repo *gogithub.Repository }
		cloneJobs := make(chan cloneJob, len(toClone))
		cloneOut := make(chan RepoResult, len(toClone))

		for i := 0; i < cloneWorkers; i++ {
			go func() {
				for j := range cloneJobs {
					select {
					case <-ctx.Done():
						cloneOut <- RepoResult{Name: j.repo.GetName(), Status: StatusError, Err: ctx.Err()}
						continue
					default:
					}
					name := j.repo.GetName()
					status := StatusCloned
					var cloneErrOut error
					cloneURL := cloneURLFor(j.repo, cfg.UseSSH)
					if err := gitRunner.Clone(baseDir, cloneURL, name); err != nil {
						status = StatusError
						cloneErrOut = err
					}
					res := RepoResult{Name: name, Status: status, Err: cloneErrOut}
					if onResult != nil {
						onResult(res)
					}
					cloneOut <- res
				}
			}()
		}

		for _, r := range toClone {
			cloneJobs <- cloneJob{repo: r}
		}
		close(cloneJobs)

		for range toClone {
			results = append(results, <-cloneOut)
		}
	}

	// Orphan detection: report local dirs with no matching GitHub repo.
	if cfg.ReportOrphans {
		knownNames := make(map[string]struct{}, len(repos))
		for _, r := range repos {
			knownNames[r.GetName()] = struct{}{}
		}
		ignoreSet := make(map[string]struct{}, len(cfg.Ignore))
		for _, name := range cfg.Ignore {
			ignoreSet[name] = struct{}{}
		}
		entries, readErr := os.ReadDir(baseDir)
		if readErr == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if _, known := knownNames[name]; known {
					continue
				}
				if _, ignored := ignoreSet[name]; ignored {
					continue
				}
				res := RepoResult{Name: name, Status: StatusOrphaned}
				results = append(results, res)
				if onResult != nil {
					onResult(res)
				}
			}
		}
	}

	// Phase 2: process existing directories.
	// Without --fetch or --pull, just mark them OK and return.
	if !cfg.Fetch && !cfg.Pull {
		for _, dir := range existingDirs {
			res := RepoResult{Name: filepath.Base(dir), Status: StatusOK}
			results = append(results, res)
			if onResult != nil {
				onResult(res)
			}
		}
		return results, nil
	}

	if len(existingDirs) == 0 {
		return results, nil
	}

	// Worker pool for existing directories.
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
				res := syncOne(ctx, cfg, gh, gitRunner, j.dir, j.repo)
				if onResult != nil {
					onResult(res)
				}
				out <- res
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

// syncOne processes a single existing repository directory.
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

	// Skip non-GitHub remotes.
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

	var wasBehind, didPull bool
	var ahead, behind int

	if isOnDefault {
		a, b, err := gitRunner.AheadBehind(dir, currentBranch, defaultBranch)
		if err == nil {
			ahead = a
			behind = b
		}
		if behind > 0 {
			wasBehind = true
			if cfg.Pull {
				if pullErr := gitRunner.PullFFOnly(dir); pullErr != nil {
					result.Status = StatusDirty
					result.Err = pullErr
					result.Branch = currentBranch
					result.DefaultBranch = defaultBranch
					result.Ahead = ahead
					result.Behind = behind
					return result
				}
				didPull = true
			}
		}
	} else {
		a, b, err := gitRunner.AheadBehind(dir, currentBranch, defaultBranch)
		if err == nil {
			ahead = a
			behind = b
		}
	}

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
		DidPull:       didPull,
	}

	decided := Decide(in)
	decided.Name = name
	decided.DefaultBranch = defaultBranch

	// With --checkout: switch SYNCED repos to the default branch and pull.
	if cfg.Checkout && decided.Status == StatusSynced {
		// Re-verify dirty state immediately before any working-tree writes.
		// If the check errors or the tree is dirty, skip checkout rather than risk data loss.
		dirtyNow, dirtyCheckErr := gitRunner.StatusDirty(dir)
		if dirtyCheckErr == nil && !dirtyNow {
			if err := gitRunner.CheckoutBranch(dir, defaultBranch); err != nil {
				decided.Status = StatusError
				decided.Err = err
				return decided
			}
			// Reflect switched HEAD immediately so subsequent errors report end state.
			decided.Branch = defaultBranch
			decided.Ahead = 0
			decided.Behind = 0
			if err := gitRunner.PullFFOnly(dir); err != nil {
				decided.Status = StatusError
				decided.Err = err
				return decided
			}
			// Update result to reflect the new HEAD state.
			decided.Branch = defaultBranch
			decided.Ahead = 0
			decided.Behind = 0
			decided.Status = StatusOK
		}
	}

	// With --fetch or --pull: report local branches that are merged or gone.
	if cfg.Fetch || cfg.Pull {
		decided.StaleBranches = staleBranches(gitRunner, dir, defaultBranch)

		if cfg.PruneMerged && cfg.Pull && decided.Branch == decided.DefaultBranch {
			var pruned []string
			var stale []string
			for _, b := range decided.StaleBranches {
				if err := gitRunner.DeleteBranch(dir, b, false); err == nil {
					pruned = append(pruned, b)
				} else {
					stale = append(stale, b)
				}
			}
			decided.PrunedBranches = pruned
			decided.StaleBranches = stale
		}
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

// staleBranches returns local branch names that are fully merged into origin's
// defaultBranch or whose remote tracking ref is gone. It never modifies the repo.
func staleBranches(gitRunner git.Runner, dir, defaultBranch string) []string {
	seen := map[string]struct{}{}

	if merged, err := gitRunner.MergedBranches(dir, defaultBranch); err == nil {
		for _, b := range merged {
			if b != defaultBranch {
				seen[b] = struct{}{}
			}
		}
	}
	if gone, err := gitRunner.GoneBranches(dir); err == nil {
		for _, b := range gone {
			if b != defaultBranch {
				seen[b] = struct{}{}
			}
		}
	}

	var result []string
	for b := range seen {
		result = append(result, b)
	}
	sort.Strings(result)
	return result
}

// safeRepoName validates that a repository name from the GitHub API is safe to
// use as a filesystem directory name under baseDir.
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
