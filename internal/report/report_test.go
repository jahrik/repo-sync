package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/jahrik/repo-sync/internal/sync"
)

func TestPrintEmpty(t *testing.T) {
	var buf bytes.Buffer
	Print(nil, &buf)
	if !strings.Contains(buf.String(), "No repositories") {
		t.Errorf("expected 'No repositories' message, got %q", buf.String())
	}
}

func TestPrintSummaryLine(t *testing.T) {
	results := []sync.RepoResult{
		{Name: "repo-a", Status: sync.StatusOK},
		{Name: "repo-b", Status: sync.StatusOK},
		{Name: "repo-c", Status: sync.StatusBehind, Behind: 2},
		{Name: "repo-d", Status: sync.StatusCloned},
	}
	var buf bytes.Buffer
	Print(results, &buf)
	out := buf.String()

	if !strings.Contains(out, "4 repos") {
		t.Errorf("expected '4 repos' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "OK 2") {
		t.Errorf("expected 'OK 2' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "BEHIND 1") {
		t.Errorf("expected 'BEHIND 1' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "CLONED 1") {
		t.Errorf("expected 'CLONED 1' in output, got:\n%s", out)
	}
}

func TestPrintWarnings(t *testing.T) {
	results := []sync.RepoResult{
		{Name: "repo-x", Status: sync.StatusOpenPR, PRNumber: 1, PRTitle: "WIP"},
		{Name: "repo-y", Status: sync.StatusUnmerged, Ahead: 3},
		{Name: "repo-z", Status: sync.StatusDirty},
	}
	var buf bytes.Buffer
	Print(results, &buf)
	out := buf.String()

	if !strings.Contains(out, "Warnings:") {
		t.Errorf("expected Warnings section, got:\n%s", out)
	}
	if !strings.Contains(out, "open pull requests") {
		t.Errorf("expected open PR warning, got:\n%s", out)
	}
	if !strings.Contains(out, "unmerged commits") {
		t.Errorf("expected unmerged warning, got:\n%s", out)
	}
	if !strings.Contains(out, "uncommitted") {
		t.Errorf("expected dirty warning, got:\n%s", out)
	}
}

func TestFormatResultAllBranches(t *testing.T) {
	tests := []struct {
		name     string
		result   sync.RepoResult
		contains []string
		absent   []string
	}{
		{
			name:   "OK no branch detail",
			result: sync.RepoResult{Name: "r", Status: sync.StatusOK, Branch: "main", DefaultBranch: "main"},
			// OK never shows branch detail
			absent: []string{"main"},
		},
		{
			name:   "Cloned no branch detail",
			result: sync.RepoResult{Name: "r", Status: sync.StatusCloned, Branch: "main", DefaultBranch: "main"},
			absent: []string{"main"},
		},
		{
			name:   "Behind on default shows count and branch",
			result: sync.RepoResult{Name: "r", Status: sync.StatusBehind, Behind: 3, Branch: "main", DefaultBranch: "main"},
			// On default branch: just branch name + count (no arrow)
			contains: []string{"main", "↓3"},
		},
		{
			name:     "Behind zero shows branch only",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusBehind, Behind: 0, Branch: "main", DefaultBranch: "main"},
			contains: []string{"main"},
			absent:   []string{"↓0"},
		},
		{
			name:     "OpenPR on feature branch shows arrow and PR",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusOpenPR, PRNumber: 7, PRTitle: "My PR", Ahead: 2, Branch: "feat", DefaultBranch: "main"},
			contains: []string{"feat", "→", "main", "#7 My PR", "+2"},
		},
		{
			name:     "OpenPR zero ahead no ahead count",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusOpenPR, PRNumber: 5, PRTitle: "x", Ahead: 0, Branch: "feat", DefaultBranch: "main"},
			contains: []string{"feat", "#5 x"},
			absent:   []string{"+0"},
		},
		{
			name:     "Unmerged shows ahead and no PR",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusUnmerged, Ahead: 4, Branch: "wip", DefaultBranch: "main"},
			contains: []string{"wip", "→", "main", "+4 ahead", "no PR"},
		},
		{
			name:     "Unmerged zero ahead still shows no PR",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusUnmerged, Ahead: 0, Branch: "wip", DefaultBranch: "main"},
			contains: []string{"wip", "no PR"},
			absent:   []string{"+0 ahead"},
		},
		{
			name:     "Dirty uncommitted shows uncommitted label",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusDirty, Branch: "main", DefaultBranch: "main"},
			contains: []string{"main", "uncommitted"},
		},
		{
			name:     "Dirty diverged shows counts and diverged label",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusDirty, Err: fmt.Errorf("pull failed"), Ahead: 2, Behind: 3, Branch: "main", DefaultBranch: "main"},
			contains: []string{"main", "↑2 ↓3", "diverged"},
		},
		{
			name:     "Error with message",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusError, Err: fmt.Errorf("boom"), Branch: "main", DefaultBranch: "main"},
			contains: []string{"main", "ERR:", "boom"},
		},
		{
			name:     "Error with nil err shows branch only",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusError, Branch: "main", DefaultBranch: "main"},
			contains: []string{"main"},
			absent:   []string{"ERR:"},
		},
		{
			name:     "Synced on feature shows arrow",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusSynced, Branch: "feat", DefaultBranch: "main"},
			contains: []string{"feat", "→", "main"},
		},
		{
			name:     "Stale branches listed",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusOK, StaleBranches: []string{"old-branch", "other"}},
			contains: []string{"stale:", "old-branch", "other"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			Print([]sync.RepoResult{tc.result}, &buf)
			out := buf.String()
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output, got:\n%s", want, out)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(out, absent) {
					t.Errorf("did not expect %q in output, got:\n%s", absent, out)
				}
			}
		})
	}
}

func TestPrintSortOrder(t *testing.T) {
	results := []sync.RepoResult{
		{Name: "z-ok", Status: sync.StatusOK},
		{Name: "a-cloned", Status: sync.StatusCloned},
		{Name: "m-dirty", Status: sync.StatusDirty},
		{Name: "b-cloned", Status: sync.StatusCloned},
	}
	var buf bytes.Buffer
	Print(results, &buf)
	out := buf.String()

	idxACloned := strings.Index(out, "a-cloned")
	idxBCloned := strings.Index(out, "b-cloned")
	idxZOK := strings.Index(out, "z-ok")
	idxMDirty := strings.Index(out, "m-dirty")

	if idxACloned > idxBCloned {
		t.Error("a-cloned should come before b-cloned")
	}
	if idxBCloned > idxZOK {
		t.Error("cloned repos should come before OK")
	}
	if idxZOK > idxMDirty {
		t.Error("OK should come before DIRTY")
	}
}
