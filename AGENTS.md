# AGENTS.md — repo-sync

Go CLI tool that syncs GitHub repos to a local directory. Shells out to `git` for all operations; uses `go-github/v72` for the GitHub API.

## Build and test

```bash
go build ./...
go vet ./...
go test -race -timeout 60s ./...
```

Go is at `~/.local/go/bin/go` on the Steam Deck. Use `PATH="$HOME/.local/go/bin:$PATH"` if `go` is not in PATH.

## Architecture

```
main.go
cmd/root.go          — cobra entry point, flag parsing, wires up real implementations
internal/config/     — resolves token + dir from flags / env / gh CLI hosts.yml
internal/github/     — GitHub API client (Client interface + real implementation)
internal/git/        — git operations (Runner interface + real implementation)
internal/sync/       — orchestration: Run(), syncOne(), Decide(), helpers
internal/report/     — formats and prints results
internal/testutil/   — fake git binary harness for integration-style tests
```

### Key design decisions

**Interfaces for testability, not go-git.** Both `git.Runner` and `github.Client` are interfaces. Tests use a `FakeRunner` struct (in `internal/sync/sync_test.go`) and a fake GitHub client. This avoids go-git's large dependency footprint and keeps tests fast and hermetic.

**Fake git binary pattern.** `internal/testutil` builds a real `git` binary stub from Go source and puts it on PATH during tests. This lets git-package tests exercise the shell-out path without a real git repo. See `testutil.go` for how the fake binary is compiled and registered.

**Pure decision function.** `internal/sync/decision.go` contains `Decide()` — a pure function with no I/O. All state gathering happens in `syncOne()` before calling `Decide()`. This makes the decision logic independently testable.

**Worker pool without WaitGroup.** `sync.Run()` uses buffered channels sized to `len(existingDirs)`. The main goroutine drains exactly that many results; workers exit when the jobs channel is closed. No WaitGroup needed because channel drain serves as the join point. Worker goroutines are leaked to GC but the process exits after the report anyway.

**CLEANED uses `-D` (force delete).** `git.Runner.DeleteLocalBranch` calls `git branch -D`. The caller (`syncOne`) is responsible for checking merge status via the decision logic before invoking delete; `-D` avoids a spurious failure if git's own merge check uses a different base.

## Decision table

| Situation | Status |
|-----------|--------|
| On default, up to date | OK |
| On default, was behind origin | BEHIND (after pulling) |
| On default, dirty working tree | DIRTY |
| On feature branch, open PR | OPEN PR |
| On feature branch, 0 commits ahead, no open PR | CLEANED |
| On feature branch, commits ahead, merged PR exists | CLEANED |
| On feature branch, commits ahead, no PR at all | UNMERGED |

## CI / CD

- `.github/workflows/ci.yml` — triggers on `pull_request` only; runs `go vet`, `go test -race`, golangci-lint, build
- `.github/workflows/release.yml` — triggers on `v*` tags; runs GoReleaser for linux/darwin amd64/arm64
- `.goreleaser.yml` — GoReleaser config; archives include LICENSE and README

## Non-obvious gotchas

- `config.Resolve` reads `~/.config/gh/hosts.yml` to pick up the token and SSH preference from the `gh` CLI automatically. If `gh` is not installed or not logged in, it falls back to unauthenticated API calls (60 req/hr rate limit).
- Repos whose `origin` remote URL does not contain `github.com` are silently skipped and reported as OK. This prevents noise from non-GitHub repos in the sync directory.
- The GitHub client's `ListMergedPRs` filters the "closed" state API response down to actually-merged PRs by checking `MergedAt != zero`. A closed-but-not-merged PR does not trigger CLEANED.
- Coverage artifacts (`cov_*.out`, `coverage.*`) are in the working tree but not tracked by git (see `.gitignore`).
