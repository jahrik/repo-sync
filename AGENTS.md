# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and test

```bash
go build ./...
go vet ./...
go test ./...                                      # CGO disabled on this machine; no -race locally
go test ./internal/sync/ -run TestRunFetchReports  # run a single test
gofmt -l .                                         # check formatting
gofmt -w .                                         # fix formatting
```

CI runs `go test -race ./...` and `golangci-lint`. Always run `gofmt -l .` before pushing.

Go is at `~/.local/go/bin/go` on this machine. Use `PATH="$HOME/.local/go/bin:$PATH"` if `go` is not in PATH.

## Architecture

```
main.go
cmd/root.go          — cobra entry point, flag wiring, progress bar, exit codes
internal/config/     — Config struct, flag→config resolution, file config loader
internal/github/     — Client interface + real GitHub API implementation
internal/git/        — Runner interface + real shell-out implementation
internal/sync/       — orchestration (Run), per-repo logic (syncOne), decision table (Decide)
internal/report/     — text and JSON output formatters
internal/testutil/   — fake git binary harness for git-package integration tests
```

### Operating modes

Three modes, selected by flags:

| Mode | Flags | What it does |
|------|-------|-------------|
| Clone-only | *(none)* | Clones missing repos; marks existing as OK without touching them |
| Fetch | `--fetch` | Fetches existing repos, reports full status; **no working-tree writes** |
| Pull | `--pull` | Fetch + `git pull --ff-only` on default branch if behind (implies `--fetch`) |
| Checkout | `--checkout` | Pull + `git checkout <default>` on SYNCED repos (implies `--pull`) |

`cfg.Fetch = flagFetch || flagPull || flagCheckout` and `cfg.Pull = flagPull || flagCheckout` — checkout implies pull implies fetch. Phase 2 (existing repos) is skipped entirely unless `cfg.Fetch || cfg.Pull`.

### Key design decisions

**Two-phase concurrent execution in `sync.Run()`:**
- Phase 1: clone missing repos concurrently (worker pool, `NumCPU×4` cap)
- Phase 2: process existing repos concurrently (same pattern) — only runs with `--fetch`/`--pull`
- Both phases call the `onResult` callback from worker goroutines; callers must protect shared state with a mutex

**`Decide()` is pure:** `internal/sync/decision.go` contains no I/O. All git state is gathered in `syncOne()` first, passed as `DecisionInput`, and a `RepoResult` is returned. Decision logic is independently unit-testable.

**`--pull` is `--ff-only` only:** Diverged histories are reported as `DIRTY` and left alone. The tool never creates merge commits or rebases.

**Interfaces for testability:** Both `git.Runner` and `githubclient.Client` are interfaces. Tests in `internal/sync/` use fakes defined in `sync_test.go` (shared across all three test files in the package via the `package sync` internal test pattern). `internal/testutil` builds a real git binary stub for git-package tests.

**Progress callbacks:** `Run()` takes two optional nil-safe callbacks — `onStart(total, clones, existing int)` and `onResult(RepoResult)`. The `cmd/` layer wraps them with a `sync.Mutex` to protect the progress bar from concurrent Phase 2 goroutines.

**Exit codes** via sentinel `exitError` type in `cmd/root.go`: 0 = clean, 1 = hard error, 2 = dirty/unmerged, 3 = open PRs.

### Config layering (highest priority first)

CLI flag → `.repo-sync.yml` (CWD) or `~/.config/repo-sync/config.yml` → `GITHUB_TOKEN` env → `~/.config/gh/hosts.yml` → `gh auth token` (keychain/encrypted storage)

`FileConfig` uses pointer fields so unset YAML keys don't override flag defaults. `cmd.Flags().Changed(name)` detects whether the user explicitly set a flag before applying file values.

### Decision table

| Situation | Status |
|-----------|--------|
| On default branch, up to date | `OK` |
| On default branch, was behind and fast-forward pulled (`--pull`) | `PULLED` |
| On default branch, behind but not pulled (`--fetch` mode) | `BEHIND` |
| On default branch, dirty working tree | `DIRTY` |
| On feature branch, open PR | `OPEN PR` |
| On feature branch, 0 commits ahead, no open PR | `SYNCED` |
| On feature branch, commits ahead, merged PR exists | `SYNCED` |
| On feature branch, commits ahead, no PR at all | `UNMERGED` |

`SYNCED` means the branch is safe to switch away from — the tool reports it but does not delete branches or check out the default branch automatically.

`BEHIND` vs `PULLED`: `BEHIND` means the repo is still behind (seen under `--fetch`). `PULLED` means it was behind and the fast-forward succeeded (seen under `--pull`). Both are treated as clean for exit-code purposes.

## Non-obvious gotchas

- `config.Resolve` reads `~/.config/gh/hosts.yml` for the token and SSH preference, falling back to `gh auth token` for keychain-based storage. A valid token is required; `NewClient` returns `ErrNoToken` with setup instructions if none is found.
- `--owner` defaults to the authenticated user's login (from `GET /user` in `NewClient`). Owner filtering runs in `sync.Run` after `ListRepos` — non-matching repos are dropped before any I/O.
- Repos whose `origin` remote does not contain `github.com` are silently skipped and reported as `OK`.
- `ListMergedPRs` filters the "closed" state API response by checking `MergedAt != zero` — a closed-but-not-merged PR does not trigger `SYNCED`.
- `--report-orphans` scans `baseDir` for directories not in the API repo list and runs **before** Phase 2, so it works even without `--fetch`/`--pull`.
- Stale local branches (merged into origin or with a deleted remote tracking ref) are **reported** in the output as `(stale branches: ...)` under `--fetch` and `--pull`, but never deleted.

## CI / CD

- `.github/workflows/ci.yml` — triggers on `pull_request`; runs `go vet`, `go test -race`, golangci-lint, build
- `.github/workflows/release.yml` — triggers on `v*` tags; runs GoReleaser for linux/darwin amd64/arm64
- `.goreleaser.yml` — GoReleaser config; archives include LICENSE and README
