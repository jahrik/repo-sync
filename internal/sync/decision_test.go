package sync

import (
	"testing"

	gogithub "github.com/google/go-github/v72/github"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func openPR(number int, title string) *gogithub.PullRequest {
	return &gogithub.PullRequest{Number: intPtr(number), Title: strPtr(title)}
}

func mergedPR(number int) *gogithub.PullRequest {
	now := gogithub.Timestamp{}
	// A non-zero MergedAt marks it as merged in client.go; for decision.go we
	// just care that the slice is non-empty.
	_ = now
	return &gogithub.PullRequest{Number: intPtr(number)}
}

func TestDecideOnDefaultClean(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "main",
		DefaultBranch: "main",
		IsOnDefault:   true,
		IsDirty:       false,
		WasBehind:     false,
	}
	got := Decide(in)
	if got.Status != StatusOK {
		t.Errorf("status = %q, want OK", got.Status)
	}
}

func TestDecideOnDefaultDirty(t *testing.T) {
	in := DecisionInput{IsOnDefault: true, IsDirty: true}
	got := Decide(in)
	if got.Status != StatusDirty {
		t.Errorf("status = %q, want DIRTY", got.Status)
	}
}

func TestDecideOnDefaultBehind(t *testing.T) {
	in := DecisionInput{IsOnDefault: true, WasBehind: true}
	got := Decide(in)
	if got.Status != StatusBehind {
		t.Errorf("status = %q, want BEHIND", got.Status)
	}
}

func TestDecideFeatureBranchOpenPR(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "feature/foo",
		DefaultBranch: "main",
		IsOnDefault:   false,
		Ahead:         3,
		OpenPRs:       []*gogithub.PullRequest{openPR(42, "Add foo")},
	}
	got := Decide(in)
	if got.Status != StatusOpenPR {
		t.Errorf("status = %q, want OPEN PR", got.Status)
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", got.PRNumber)
	}
	if got.PRTitle != "Add foo" {
		t.Errorf("PRTitle = %q, want 'Add foo'", got.PRTitle)
	}
	if got.Ahead != 3 {
		t.Errorf("Ahead = %d, want 3", got.Ahead)
	}
}

func TestDecideFeatureBranchNoAheadNoPR(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "feature/old",
		DefaultBranch: "main",
		IsOnDefault:   false,
		Ahead:         0,
	}
	got := Decide(in)
	if got.Status != StatusCleaned {
		t.Errorf("status = %q, want CLEANED", got.Status)
	}
}

func TestDecideFeatureBranchAheadMergedPR(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "feature/done",
		DefaultBranch: "main",
		IsOnDefault:   false,
		Ahead:         2,
		MergedPRs:     []*gogithub.PullRequest{mergedPR(10)},
	}
	got := Decide(in)
	if got.Status != StatusCleaned {
		t.Errorf("status = %q, want CLEANED", got.Status)
	}
}

func TestDecideFeatureBranchUnmerged(t *testing.T) {
	in := DecisionInput{
		CurrentBranch: "feature/wip",
		DefaultBranch: "main",
		IsOnDefault:   false,
		Ahead:         5,
	}
	got := Decide(in)
	if got.Status != StatusUnmerged {
		t.Errorf("status = %q, want UNMERGED", got.Status)
	}
}

// TestDecideTableDriven covers every decision branch exhaustively.
func TestDecideTableDriven(t *testing.T) {
	tests := []struct {
		name       string
		in         DecisionInput
		wantStatus Status
		wantAhead  int
		wantBehind int
		wantPRNum  int
		wantPRTitle string
	}{
		{
			name:       "default branch clean",
			in:         DecisionInput{CurrentBranch: "main", IsOnDefault: true},
			wantStatus: StatusOK,
		},
		{
			name:       "default branch dirty",
			in:         DecisionInput{CurrentBranch: "main", IsOnDefault: true, IsDirty: true},
			wantStatus: StatusDirty,
		},
		{
			name:       "default branch was behind",
			in:         DecisionInput{CurrentBranch: "main", IsOnDefault: true, WasBehind: true, Behind: 2},
			wantStatus: StatusBehind,
			wantBehind: 2,
		},
		{
			name:       "feature branch open PR ahead=0 (PR wins over ahead check)",
			in:         DecisionInput{CurrentBranch: "feat", IsOnDefault: false, Ahead: 0, OpenPRs: []*gogithub.PullRequest{openPR(1, "title")}},
			wantStatus: StatusOpenPR,
			wantPRNum:  1,
			wantPRTitle: "title",
			wantAhead:  0,
		},
		{
			name:       "feature branch open PR ahead>0",
			in:         DecisionInput{CurrentBranch: "feat", IsOnDefault: false, Ahead: 3, OpenPRs: []*gogithub.PullRequest{openPR(9, "big PR")}},
			wantStatus: StatusOpenPR,
			wantPRNum:  9,
			wantPRTitle: "big PR",
			wantAhead:  3,
		},
		{
			name:       "feature branch no PR no ahead → cleaned",
			in:         DecisionInput{CurrentBranch: "feat", IsOnDefault: false, Ahead: 0},
			wantStatus: StatusCleaned,
		},
		{
			name:       "feature branch ahead with merged PR → cleaned",
			in:         DecisionInput{CurrentBranch: "feat", IsOnDefault: false, Ahead: 2, MergedPRs: []*gogithub.PullRequest{mergedPR(3)}},
			wantStatus: StatusCleaned,
		},
		{
			name:       "feature branch ahead no PR → unmerged",
			in:         DecisionInput{CurrentBranch: "feat", IsOnDefault: false, Ahead: 7},
			wantStatus: StatusUnmerged,
		},
		{
			name:       "branch and ahead/behind propagated",
			in:         DecisionInput{CurrentBranch: "fix/bug", IsOnDefault: false, Ahead: 4, Behind: 1},
			wantStatus: StatusUnmerged,
			wantAhead:  4,
			wantBehind: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.in)
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tc.wantStatus)
			}
			if tc.wantPRNum != 0 && got.PRNumber != tc.wantPRNum {
				t.Errorf("PRNumber = %d, want %d", got.PRNumber, tc.wantPRNum)
			}
			if tc.wantPRTitle != "" && got.PRTitle != tc.wantPRTitle {
				t.Errorf("PRTitle = %q, want %q", got.PRTitle, tc.wantPRTitle)
			}
			if tc.wantAhead != 0 && got.Ahead != tc.wantAhead {
				t.Errorf("Ahead = %d, want %d", got.Ahead, tc.wantAhead)
			}
			if tc.wantBehind != 0 && got.Behind != tc.wantBehind {
				t.Errorf("Behind = %d, want %d", got.Behind, tc.wantBehind)
			}
			if got.Branch != tc.in.CurrentBranch {
				t.Errorf("Branch = %q, want %q", got.Branch, tc.in.CurrentBranch)
			}
		})
	}
}
