# repo-sync

Syncs all your GitHub repositories to a local directory. By default it clones
any repos you don't have locally. With `--fetch`, `--pull`, or `--checkout` it
also inspects existing repos and reports their status.

## Install

```bash
go install github.com/jahrik/repo-sync@latest
```

Or download a pre-built binary from the [releases page](https://github.com/jahrik/repo-sync/releases).

## Authentication

repo-sync requires a GitHub personal access token to list your repositories and
query PR status. Without a valid token the tool will not start.

### Quickstart (recommended)

If you already have the [GitHub CLI](https://cli.github.com/) installed:

```bash
gh auth login
```

repo-sync automatically detects your `gh` token (whether stored in the
system keychain or `~/.config/gh/hosts.yml`). No further configuration needed.

### Alternative methods

repo-sync resolves a token in this order (first match wins):

1. `--token` flag
2. `token` field in config file (`.repo-sync.yml` or `~/.config/repo-sync/config.yml`)
3. `GITHUB_TOKEN` environment variable
4. `~/.config/gh/hosts.yml` (gh CLI file-based storage)
5. `gh auth token` (gh CLI keychain/encrypted storage)

### Required scopes

The token needs the `repo` scope (or `public_repo` for public repos only).

## Usage

```
repo-sync [flags]

Flags:
      --dir string       directory containing your local clones (default "~/github")
      --fetch            fetch existing repos and report status (no writes)
      --pull             fetch and fast-forward pull existing repos (implies --fetch)
      --checkout         switch SYNCED repos to the default branch and pull (implies --pull)
      --skip-forks       exclude forked repositories
      --skip-archived    exclude archived repositories
      --report-orphans   report local directories with no matching GitHub repo
      --format string    output format: text or json (default "text")
      --filter string    regexp to filter repos by name (empty = all)
      --owner string     GitHub user or org to sync (default: authenticated user)
      --limit int        maximum number of repositories to fetch (default 200)
      --token string     GitHub personal access token
  -h, --help             help for repo-sync
      --version          version for repo-sync
```

## Modes

### Default — clone only

```bash
repo-sync
```

Fetches the repository list from GitHub and clones any repos not already present
locally. Existing local directories are reported as `OK` without being touched.
No network traffic beyond the API call and any clones.

### `--fetch` — read-only status

```bash
repo-sync --fetch
```

Runs `git fetch --prune` on every existing repo and reports full branch status
(behind, ahead, dirty, open PR, unmerged). **Makes no changes to your working
tree.** Safe to run at any time — equivalent to running `git fetch` across all
your repos and reading the results. Also reports stale local branches
(merged or tracking-gone) without touching them.

Use `--fetch` when you want a status snapshot without committing to any pulls.

### `--pull` — fast-forward pull

```bash
repo-sync --pull
```

Everything `--fetch` does, plus runs `git pull --ff-only` on repos whose default
branch is behind origin. The `--ff-only` flag means the pull is refused if the
history has diverged — repo-sync will never create a merge commit or silently
rebase. A diverged repo is reported as `DIRTY` so you can resolve it manually.

`--pull` implies `--fetch`.

### `--checkout` — clean up SYNCED repos

```bash
repo-sync --checkout
```

Everything `--pull` does, plus automatically switches `SYNCED` repos back to
their default branch and pulls. A repo is `SYNCED` when it is on a feature
branch with no commits ahead of the default branch and no open PR — meaning the
branch work is already merged and the checkout is safe.

`--checkout` implies `--pull` (and therefore `--fetch`). If the working tree is
dirty the checkout is skipped rather than risking data loss.

## How modes relate to git

| repo-sync flag | Equivalent git behaviour |
|----------------|--------------------------|
| *(none)*       | `git clone` for missing repos only |
| `--fetch`      | `git fetch --prune` — updates remote refs, no working-tree changes |
| `--pull`       | `git fetch --prune` + `git pull --ff-only` on the default branch |
| `--checkout`   | `--pull` + `git checkout <default>` on SYNCED repos |

Key differences from plain `git pull`:

- **Default branch only.** repo-sync only pulls the default branch (`main`,
  `master`, etc.). If you are on a feature branch it is reported but not
  touched.
- **Fast-forward only.** We use `--ff-only`, so diverged histories are flagged
  rather than merged or rebased. This is intentionally more conservative than
  git's default pull behaviour.
- **Read-only by default.** Without `--pull`, `--fetch` makes no working-tree
  changes at all.

## Options

### `--skip-forks` / `--skip-archived`

Exclude forked or archived repositories from the sync entirely:

```bash
repo-sync --skip-forks --skip-archived
```

### `--report-orphans`

Report local directories under `--dir` that have no matching GitHub repository.
Useful for spotting repos you deleted on GitHub but still have locally:

```bash
repo-sync --report-orphans
```

### `--filter`

Process only repos whose name matches a regular expression. Applied after
`--skip-forks` and `--skip-archived`:

```bash
repo-sync --fetch --filter '^ansible-'   # repos starting with "ansible-"
repo-sync --pull  --filter 'api|gateway' # repos containing "api" or "gateway"
```

Plain substrings work too — the pattern is anchored nowhere, so `--filter foo`
matches any repo whose name contains `foo`.

### `--format json`

Output results as a JSON array instead of the text report. Useful for scripting:

```bash
repo-sync --fetch --format json | jq '.[] | select(.status == "BEHIND")'
```

Each object includes: `name`, `status`, `branch`, `default_branch`, `ahead`, `behind`,
`pr_number`, `pr_title`, `stale_branches`, and `error` (all omitted when zero/empty).

## Config file

repo-sync looks for a config file in two places (first match wins):

1. `.repo-sync.yml` in the current directory
2. `~/.config/repo-sync/config.yml`

Any flag can be set in the config file. CLI flags always override the file.

```yaml
# .repo-sync.yml
dir: ~/github
limit: 200
skip_forks: true
skip_archived: true
# fetch: true
# pull: true
# checkout: true
# owner: myorg
# format: text
# report_orphans: false
```

## Status codes

| Status      | Meaning |
|-------------|---------|
| `OK`        | Existing repo, not inspected (default mode) or up to date (`--fetch`/`--pull`) |
| `CLONED`    | Repository was not present locally and has been cloned |
| `PULLED`    | Default branch was behind origin and was fast-forward pulled (`--pull` only) |
| `BEHIND`    | Default branch is behind origin but was not pulled (`--fetch` mode) |
| `SYNCED`    | On a feature branch with no commits ahead and no open PR — safe to switch back to default |
| `OPEN PR`   | On a feature branch with an open pull request |
| `UNMERGED`  | On a feature branch with commits ahead of default and no open or merged PR |
| `DIRTY`     | Working tree has uncommitted changes, or `--ff-only` pull failed (diverged history) |
| `ORPHANED`  | Local directory has no matching GitHub repo (`--report-orphans` only) |
| `ERROR`     | An unexpected error occurred (message shown inline) |

When a repo has stale local branches (merged into origin or with a deleted
remote tracking ref), they are listed inline in the report as
`stale: branch-name, ...`. No branches are deleted automatically.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | All repos are clean (OK, CLONED, PULLED, BEHIND, SYNCED) |
| `1`  | One or more repos had a hard error |
| `2`  | One or more repos have dirty working trees or unmerged branches |
| `3`  | One or more repos have open pull requests |

## Sample output

```
  cloned   new-project
  cloned   another-new-repo

Found 42 repos (2 to clone, 40 already present)
Done — cloned 2 new repo(s).

  CLONED     another-new-repo
  CLONED     new-project
  PULLED     my-lib  ·  main  ·  ↓3
  BEHIND     other-lib  ·  main  ·  ↓1
  SYNCED     old-feature  ·  old-feature → main  ·  stale: old-feature
  OK         dotfiles
  OPEN PR    my-feature  ·  my-feature → main  ·  #42 Add widget support  ·  +2
  UNMERGED   spike  ·  spike → main  ·  +5 ahead  ·  no PR
  DIRTY      local-work  ·  main  ·  uncommitted
  ORPHANED   deleted-repo

  ╭──────────────────────────────────────────────────────────────────────────────────────╮
  │ 10 repos  ·  CLONED 2  ·  PULLED 1  ·  BEHIND 1  ·  SYNCED 1  ·  OK 1  ·  OPEN PR 1  ·  UNMERGED 1  ·  DIRTY 1  ·  ORPHANED 1 │
  ╰──────────────────────────────────────────────────────────────────────────────────────╯

Warnings:
  !  1 repo(s) have open pull requests
  !  1 repo(s) have unmerged commits with no PR
  !  1 repo(s) have uncommitted changes or diverged history
  !  1 local director(ies) have no matching GitHub repo
```

## How it works

1. Resolves config from file, environment, and flags (flags win).
2. Fetches the repository list from the GitHub API, then filters by `--owner`,
   `--skip-forks`, and `--skip-archived`.
3. **Phase 1 — clone:** splits repos into "missing" and "existing". Clones
   missing repos concurrently (worker pool, up to `NumCPU×4` workers).
4. If `--report-orphans` is set, scans the local directory for directories not
   in the API list and marks them `ORPHANED`.
5. **Phase 2 — sync** (only with `--fetch`, `--pull`, or `--checkout`): processes
   existing repos concurrently via a worker pool. Each worker:
   - Runs `git fetch --prune`
   - Checks current branch, ahead/behind count, dirty state
   - Queries the GitHub API for open and merged PRs (only on non-default branches)
   - With `--pull`: runs `git pull --ff-only` if the default branch is behind
   - With `--checkout`: also switches SYNCED repos to the default branch and pulls
   - Reports stale local branches (merged or tracking-gone) informally
6. Prints a sorted, annotated report to stdout (or JSON with `--format json`).
   Progress and human-friendly notifications go to stderr.

Repos whose `origin` remote is not on `github.com` are skipped silently
(reported as `OK`).
