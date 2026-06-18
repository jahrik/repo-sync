package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNotGitHub is returned by RemoteURL when the remote is not a github.com URL.
var ErrNotGitHub = errors.New("git: remote is not a github.com URL")

// Runner abstracts git operations so they can be faked in tests.
type Runner interface {
	IsGitRepo(dir string) bool
	FetchPrune(dir string) error
	DefaultBranch(dir string) (string, error)
	CurrentBranch(dir string) (string, error)
	RemoteURL(dir string) (string, error)
	AheadBehind(dir, branch, defaultBranch string) (ahead, behind int, err error)
	Checkout(dir, branch string) error
	PullFFOnly(dir string) error
	MergedBranches(dir, defaultBranch string) ([]string, error)
	GoneBranches(dir string) ([]string, error)
	StatusDirty(dir string) (bool, error)
	Clone(parentDir, cloneURL, name string) error
}

type runner struct{}

// NewRunner returns a Runner that shells out to the system git binary.
func NewRunner() Runner { return &runner{} }

// run executes a git subcommand in dir and returns combined output.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		sub := ""
		if len(args) > 0 {
			sub = args[0]
		}
		return "", fmt.Errorf("git %s in %s: %w: %s", sub, dir, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *runner) IsGitRepo(dir string) bool {
	_, err := run(dir, "rev-parse", "--git-dir")
	return err == nil
}

func (r *runner) FetchPrune(dir string) error {
	_, err := run(dir, "fetch", "--prune", "--quiet", "origin")
	if err != nil {
		return fmt.Errorf("git fetch in %s: %w", dir, err)
	}
	return nil
}

func (r *runner) DefaultBranch(dir string) (string, error) {
	// Try symbolic-ref for origin/HEAD.
	out, err := run(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		// Returns "origin/main" → strip prefix.
		return strings.TrimPrefix(out, "origin/"), nil
	}

	// Fallback: check if refs/remotes/origin/main exists.
	if _, err2 := run(dir, "show-ref", "--verify", "refs/remotes/origin/main"); err2 == nil {
		return "main", nil
	}

	// Final fallback: master.
	return "master", nil
}

func (r *runner) CurrentBranch(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git CurrentBranch in %s: %w", dir, err)
	}
	return out, nil
}

func (r *runner) RemoteURL(dir string) (string, error) {
	out, err := run(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote-url in %s: %w", dir, err)
	}
	if !strings.Contains(out, "github.com") {
		return out, ErrNotGitHub
	}
	return out, nil
}

// AheadBehind returns how many commits branch is ahead/behind defaultBranch on
// origin.  It uses left-right rev-list: "behind\tahead".
func (r *runner) AheadBehind(dir, branch, defaultBranch string) (ahead, behind int, err error) {
	ref := fmt.Sprintf("origin/%s...%s", defaultBranch, branch)
	out, e := run(dir, "rev-list", "--left-right", "--count", ref)
	if e != nil {
		return 0, 0, fmt.Errorf("git AheadBehind in %s: %w", dir, e)
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("git AheadBehind in %s: unexpected output %q", dir, out)
	}
	b, err1 := strconv.Atoi(parts[0])
	a, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("git AheadBehind in %s: parse error %q", dir, out)
	}
	return a, b, nil
}

func (r *runner) Checkout(dir, branch string) error {
	_, err := run(dir, "checkout", branch)
	if err != nil {
		return fmt.Errorf("git checkout in %s: %w", dir, err)
	}
	return nil
}

func (r *runner) PullFFOnly(dir string) error {
	_, err := run(dir, "pull", "--ff-only")
	if err != nil {
		return fmt.Errorf("git pull in %s: %w", dir, err)
	}
	return nil
}

// MergedBranches lists local branches that have been fully merged into
// defaultBranch on origin.
func (r *runner) MergedBranches(dir, defaultBranch string) ([]string, error) {
	out, err := run(dir, "branch", "--merged", fmt.Sprintf("origin/%s", defaultBranch))
	if err != nil {
		return nil, fmt.Errorf("git merged-branches in %s: %w", dir, err)
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		b := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if b != "" && b != defaultBranch {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// GoneBranches lists local branches whose upstream tracking ref is gone
// (i.e., the remote branch has been deleted).
func (r *runner) GoneBranches(dir string) ([]string, error) {
	out, err := run(dir, "branch", "-vv")
	if err != nil {
		return nil, fmt.Errorf("git gone-branches in %s: %w", dir, err)
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, ": gone]") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			if name == "*" {
				if len(fields) < 2 {
					continue
				}
				name = fields[1]
			}
			if name != "" {
				branches = append(branches, name)
			}
		}
	}
	return branches, nil
}

func (r *runner) StatusDirty(dir string) (bool, error) {
	out, err := run(dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", dir, err)
	}
	return out != "", nil
}

func (r *runner) Clone(parentDir, cloneURL, name string) error {
	// "--" separates options from positional arguments, preventing a name that
	// starts with "-" from being misinterpreted as a git flag.
	_, err := run(parentDir, "clone", "--quiet", "--", cloneURL, name)
	if err != nil {
		return fmt.Errorf("git clone %s: %w", name, err)
	}
	return nil
}
