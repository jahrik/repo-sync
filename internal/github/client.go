package github

import (
	"context"
	"fmt"
	"time"

	gogithub "github.com/google/go-github/v88/github"
)

// Client abstracts the GitHub API calls used by repo-sync.
type Client interface {
	Owner() string
	ListRepos(ctx context.Context, limit int) ([]*gogithub.Repository, error)
	ListOpenPRs(ctx context.Context, owner, repo, branch string) ([]*gogithub.PullRequest, error)
	ListMergedPRs(ctx context.Context, owner, repo, branch string) ([]*gogithub.PullRequest, error)
}

type client struct {
	gh    *gogithub.Client
	owner string
}

// ErrNoToken is returned when NewClient is called with an empty token.
var ErrNoToken = &noTokenError{}

type noTokenError struct{}

func (e *noTokenError) Error() string {
	return "no GitHub token found\n\n" +
		"Provide a token via one of:\n" +
		"  1. --token flag\n" +
		"  2. token field in config file (.repo-sync.yml or ~/.config/repo-sync/config.yml)\n" +
		"  3. GITHUB_TOKEN environment variable\n" +
		"  4. gh auth login (uses ~/.config/gh/hosts.yml or system keychain)\n\n" +
		`The token needs the "repo" scope (or "public_repo" for public repos only).`
}

// NewClient creates an authenticated GitHub client and resolves the
// authenticated user's login as the owner.
func NewClient(token string) (Client, error) {
	if token == "" {
		return nil, ErrNoToken
	}

	ghc, err := gogithub.NewClient(gogithub.WithAuthToken(token))
	if err != nil {
		return nil, fmt.Errorf("github: create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	user, resp, err := ghc.Users.Get(ctx, "")
	if err != nil {
		if resp != nil && resp.StatusCode == 401 {
			return nil, fmt.Errorf("github: authentication failed (token may be invalid or expired): %w", err)
		}
		return nil, fmt.Errorf("github: resolve owner: %w", err)
	}
	owner := user.GetLogin()
	if owner == "" {
		return nil, fmt.Errorf("github: empty owner login")
	}

	return &client{gh: ghc, owner: owner}, nil
}

// Owner returns the authenticated user's GitHub login.
func (c *client) Owner() string { return c.owner }

// ListRepos returns up to limit repositories owned by the authenticated user.
func (c *client) ListRepos(ctx context.Context, limit int) ([]*gogithub.Repository, error) {
	var all []*gogithub.Repository
	opts := &gogithub.RepositoryListByAuthenticatedUserOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.gh.Repositories.ListByAuthenticatedUser(ctx, opts)
		if err != nil {
			if rateLimitErr, ok := err.(*gogithub.RateLimitError); ok {
				wait := time.Until(rateLimitErr.Rate.Reset.Time) + time.Second
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				repos, resp, err = c.gh.Repositories.ListByAuthenticatedUser(ctx, opts)
				if err != nil {
					return nil, fmt.Errorf("github: list repos (after rate-limit retry): %w", err)
				}
			} else {
				return nil, fmt.Errorf("github: list repos: %w", err)
			}
		}

		all = append(all, repos...)
		if len(all) >= limit || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ListOpenPRs returns open pull requests for the given branch in owner/repo.
func (c *client) ListOpenPRs(ctx context.Context, owner, repo, branch string) ([]*gogithub.PullRequest, error) {
	return c.listPRs(ctx, owner, repo, branch, "open")
}

// ListMergedPRs returns merged pull requests for the given branch in owner/repo.
func (c *client) ListMergedPRs(ctx context.Context, owner, repo, branch string) ([]*gogithub.PullRequest, error) {
	return c.listPRs(ctx, owner, repo, branch, "closed")
}

func (c *client) listPRs(ctx context.Context, owner, repo, branch, state string) ([]*gogithub.PullRequest, error) {
	opts := &gogithub.PullRequestListOptions{
		State:       state,
		Head:        fmt.Sprintf("%s:%s", owner, branch),
		ListOptions: gogithub.ListOptions{PerPage: 50},
	}

	var all []*gogithub.PullRequest
	for {
		prs, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			if rateLimitErr, ok := err.(*gogithub.RateLimitError); ok {
				wait := time.Until(rateLimitErr.Rate.Reset.Time) + time.Second
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				prs, resp, err = c.gh.PullRequests.List(ctx, owner, repo, opts)
				if err != nil {
					return nil, fmt.Errorf("github: list PRs (after rate-limit retry): %w", err)
				}
			} else {
				return nil, fmt.Errorf("github: list PRs: %w", err)
			}
		}

		// For "closed" state, only include merged PRs.
		if state == "closed" {
			for _, pr := range prs {
				if pr.GetMergedAt() != (gogithub.Timestamp{}) {
					all = append(all, pr)
				}
			}
		} else {
			all = append(all, prs...)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}
