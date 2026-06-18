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
	if !strings.Contains(out, "OK: 2") {
		t.Errorf("expected 'OK: 2' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "BEHIND: 1") {
		t.Errorf("expected 'BEHIND: 1' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "CLONED: 1") {
		t.Errorf("expected 'CLONED: 1' in output, got:\n%s", out)
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
	if !strings.Contains(out, "uncommitted changes") {
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
			name:   "OK no branch suffix",
			result: sync.RepoResult{Name: "r", Status: sync.StatusOK, Branch: "main"},
			absent: []string{"[branch:"},
		},
		{
			name:   "Cloned no branch suffix",
			result: sync.RepoResult{Name: "r", Status: sync.StatusCloned, Branch: "main"},
			absent: []string{"[branch:"},
		},
		{
			name:     "Behind with count and branch",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusBehind, Behind: 3, Branch: "main"},
			contains: []string{"↓3", "[branch: main]"},
		},
		{
			name:     "Behind zero shows branch only",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusBehind, Behind: 0, Branch: "main"},
			contains: []string{"[branch: main]"},
			absent:   []string{"↓0"},
		},
		{
			name:     "OpenPR with ahead and branch",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusOpenPR, PRNumber: 7, PRTitle: "My PR", Ahead: 2, Branch: "feat"},
			contains: []string{"[#7: My PR]", "(+2)", "[branch: feat]"},
		},
		{
			name:     "OpenPR zero ahead no ahead suffix",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusOpenPR, PRNumber: 5, PRTitle: "x", Ahead: 0, Branch: "feat"},
			contains: []string{"[#5: x]"},
			absent:   []string{"(+0)"},
		},
		{
			name:     "Unmerged with ahead and branch",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusUnmerged, Ahead: 4, Branch: "wip"},
			contains: []string{"+4 ahead, no PR", "[branch: wip]"},
		},
		{
			name:     "Unmerged zero ahead no suffix",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusUnmerged, Ahead: 0, Branch: "wip"},
			contains: []string{"[branch: wip]"},
			absent:   []string{"+0 ahead"},
		},
		{
			name:     "Error with err message",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusError, Err: fmt.Errorf("boom"), Branch: "main"},
			contains: []string{"ERR: boom", "[branch: main]"},
		},
		{
			name:     "Error with nil err",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusError, Branch: "main"},
			contains: []string{"[branch: main]"},
			absent:   []string{"ERR:"},
		},
		{
			name:     "Dirty shows branch",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusDirty, Branch: "main"},
			contains: []string{"[branch: main]"},
		},
		{
			name:     "Cleaned shows branch",
			result:   sync.RepoResult{Name: "r", Status: sync.StatusCleaned, Branch: "feat"},
			contains: []string{"[branch: feat]"},
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

	// a-cloned and b-cloned should appear before z-ok and m-dirty.
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
