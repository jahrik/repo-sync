package report

import (
	"bytes"
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
