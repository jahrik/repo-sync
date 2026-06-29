package sync

import gogithub "github.com/google/go-github/v88/github"

// Status is the outcome of syncing a single repository.
type Status string

const (
	StatusOK       Status = "OK"
	StatusCloned   Status = "CLONED"
	StatusPulled   Status = "PULLED" // was behind, fast-forward pulled successfully
	StatusSynced   Status = "SYNCED"
	StatusBehind   Status = "BEHIND" // is behind and was not pulled (--fetch mode)
	StatusOpenPR   Status = "OPEN PR"
	StatusUnmerged Status = "UNMERGED"
	StatusDirty    Status = "DIRTY"
	StatusOrphaned Status = "ORPHANED" // local dir has no matching GitHub repo
	StatusError    Status = "ERROR"
)

// RepoResult is the result of processing one repository.
type RepoResult struct {
	Name          string
	Status        Status
	Branch        string // current branch
	DefaultBranch string // repo's default branch
	PRNumber      int
	PRTitle       string
	Ahead         int
	Behind        int
	Err           error
	StaleBranches []string // local branches that are merged or gone (reported under --fetch/--pull)
}

// DecisionInput holds all facts needed to decide what to do with a repo.
type DecisionInput struct {
	CurrentBranch string
	DefaultBranch string
	Ahead         int
	Behind        int
	OpenPRs       []*gogithub.PullRequest
	MergedPRs     []*gogithub.PullRequest
	IsDirty       bool
	IsOnDefault   bool
	WasBehind     bool
	DidPull       bool // true when PullFFOnly succeeded
}
