package sync

import gogithub "github.com/google/go-github/v72/github"

// Status is the outcome of syncing a single repository.
type Status string

const (
	StatusOK       Status = "OK"
	StatusCloned   Status = "CLONED"
	StatusCleaned  Status = "CLEANED"
	StatusBehind   Status = "BEHIND"
	StatusOpenPR   Status = "OPEN PR"
	StatusUnmerged Status = "UNMERGED"
	StatusDirty    Status = "DIRTY"
	StatusError    Status = "ERROR"
)

// RepoResult is the result of processing one repository.
type RepoResult struct {
	Name     string
	Status   Status
	Branch   string
	PRNumber int
	PRTitle  string
	Ahead    int
	Behind   int
	Err      error
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
}
