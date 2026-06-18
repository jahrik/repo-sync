# repo-sync

Syncs all your GitHub repositories to a local directory. Clones missing repos, fast-forwards default branches, cleans up merged feature branches, and reports on open PRs and unmerged work.

## Install

```bash
go install github.com/jahrik/repo-sync@latest
```

Or download a pre-built binary from the [releases page](https://github.com/jahrik/repo-sync/releases).

## GitHub token setup

repo-sync needs read access to your repositories and PR data. It resolves a token in this order:

1. `--token` flag
2. `GITHUB_TOKEN` environment variable
3. `~/.config/gh/hosts.yml` (auto-detected if you use the `gh` CLI)

The token needs the `repo` scope (or `public_repo` for public repos only).

## Usage

```
repo-sync clones missing repositories, fast-forwards default branches,
cleans up merged feature branches, and reports on open PRs and unmerged work.

Usage:
  repo-sync [flags]

Flags:
      --dir string     directory containing your local clones (default "~/github")
  -h, --help           help for repo-sync
      --limit int      maximum number of repositories to process (default 200)
      --token string   GitHub personal access token (overrides GITHUB_TOKEN env and gh CLI config)
```

### Examples

```bash
# Sync up to 200 repos into ~/github (default)
repo-sync

# Use a different directory
repo-sync --dir ~/code

# Limit to the 50 most recently pushed repos
repo-sync --limit 50

# Pass a token explicitly
repo-sync --token ghp_xxxx
```

## Status codes

| Status     | Meaning |
|------------|---------|
| `OK`       | Default branch is up to date, no action needed |
| `CLONED`   | Repository was not present locally and was cloned |
| `BEHIND`   | Default branch was behind origin and was fast-forwarded |
| `CLEANED`  | Feature branch had been merged (or had no commits ahead); checked out default branch and deleted the local branch |
| `OPEN PR`  | On a feature branch with an open pull request |
| `UNMERGED` | On a feature branch with commits ahead of default and no open or merged PR |
| `DIRTY`    | Working tree has uncommitted changes, or fast-forward failed due to diverged history |
| `ERROR`    | An unexpected error occurred (message shown inline) |

## Sample output

```
  CLONED        new-project
  BEHIND        my-lib  (↓3)
  CLEANED       old-feature
  OK            dotfiles
  OPEN PR       my-feature  [#42: Add widget support] (+2)  [branch: my-feature]
  UNMERGED      spike  (+5 ahead, no PR)  [branch: spike]

Summary: 6 repos | CLONED: 1 | BEHIND: 1 | CLEANED: 1 | OK: 1 | OPEN PR: 1 | UNMERGED: 1

Warnings:
  ! 1 repo(s) have open pull requests
  ! 1 repo(s) have unmerged commits with no PR
```

## How it works

1. Lists all repositories for the authenticated user via the GitHub API.
2. Clones any that are missing from the local directory (sequential, in API order).
3. For each existing local directory, runs a concurrent worker pool (up to `NumCPU*4` workers) that:
   - Fetches and prunes remote refs
   - Determines if the current branch is ahead/behind the default branch
   - Queries the GitHub API for open and merged PRs (only when on a non-default branch)
   - Applies the decision table above and executes any needed git operations
4. Prints a sorted, annotated report and exits with code 0 if all repos are OK/CLONED/BEHIND/CLEANED, or code 1 if any ERROR occurred.

Repos whose `origin` remote is not on `github.com` are skipped silently (reported as OK).
