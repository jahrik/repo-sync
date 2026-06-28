package cmd

import (
	"runtime/debug"
	"testing"

	reposync "github.com/jahrik/repo-sync/internal/sync"
)

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		name     string
		results  []reposync.RepoResult
		wantCode int // 0 = nil error
	}{
		{
			name:     "empty results is clean",
			results:  nil,
			wantCode: 0,
		},
		{
			name: "all clean statuses exit 0",
			results: []reposync.RepoResult{
				{Status: reposync.StatusOK},
				{Status: reposync.StatusCloned},
				{Status: reposync.StatusPulled},
				{Status: reposync.StatusBehind},
				{Status: reposync.StatusSynced},
				{Status: reposync.StatusOrphaned},
			},
			wantCode: 0,
		},
		{
			name:     "ERROR exits 1",
			results:  []reposync.RepoResult{{Status: reposync.StatusError}},
			wantCode: 1,
		},
		{
			name:     "DIRTY exits 2",
			results:  []reposync.RepoResult{{Status: reposync.StatusDirty}},
			wantCode: 2,
		},
		{
			name:     "UNMERGED exits 2",
			results:  []reposync.RepoResult{{Status: reposync.StatusUnmerged}},
			wantCode: 2,
		},
		{
			name:     "OPEN PR exits 3",
			results:  []reposync.RepoResult{{Status: reposync.StatusOpenPR}},
			wantCode: 3,
		},
		{
			name: "ERROR beats DIRTY beats OPEN PR",
			results: []reposync.RepoResult{
				{Status: reposync.StatusOpenPR},
				{Status: reposync.StatusDirty},
				{Status: reposync.StatusError},
			},
			wantCode: 1,
		},
		{
			name: "DIRTY beats OPEN PR",
			results: []reposync.RepoResult{
				{Status: reposync.StatusOpenPR},
				{Status: reposync.StatusDirty},
			},
			wantCode: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := exitCodeFor(tc.results)
			if tc.wantCode == 0 {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected exit code %d, got nil", tc.wantCode)
			}
			var ee *exitError
			ok := false
			for e := err; e != nil; {
				if x, isEE := e.(*exitError); isEE {
					ee = x
					ok = true
					break
				}
				if u, ok2 := e.(interface{ Unwrap() error }); ok2 {
					e = u.Unwrap()
				} else {
					break
				}
			}
			if !ok {
				t.Fatalf("error is not an *exitError: %T %v", err, err)
			}
			if ee.code != tc.wantCode {
				t.Errorf("exit code: got %d, want %d", ee.code, tc.wantCode)
			}
		})
	}
}

func TestResolveVersionFrom(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef123456"},
			{Key: "vcs.time", Value: "2026-06-28T20:00:00Z"},
		},
	}

	t.Run("ldflags values pass through unchanged", func(t *testing.T) {
		version, commit, date := resolveVersionFrom(info, "v1.2.3", "abc123", "2026-06-28")
		if version != "v1.2.3" || commit != "abc123" || date != "2026-06-28" {
			t.Errorf("expected explicit values preserved, got %q %q %q", version, commit, date)
		}
	})

	t.Run("defaults are filled from build info", func(t *testing.T) {
		version, commit, date := resolveVersionFrom(info, "dev", "none", "unknown")
		if version != "v9.9.9" {
			t.Errorf("version: got %q, want v9.9.9", version)
		}
		if commit != "abcdef123456" {
			t.Errorf("commit: got %q, want abcdef123456", commit)
		}
		if date != "2026-06-28T20:00:00Z" {
			t.Errorf("date: got %q, want 2026-06-28T20:00:00Z", date)
		}
	})

	t.Run("devel module version does not override dev", func(t *testing.T) {
		devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
		version, _, _ := resolveVersionFrom(devel, "dev", "none", "unknown")
		if version != "dev" {
			t.Errorf("version: got %q, want dev", version)
		}
	})
}
