package sync

import "testing"

func TestParseOwnerRepo(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"git@github.com:jahrik/repo-sync.git", "jahrik", "repo-sync", false},
		{"git@github.com:jahrik/repo-sync", "jahrik", "repo-sync", false},
		{"https://github.com/jahrik/repo-sync.git", "jahrik", "repo-sync", false},
		{"https://github.com/jahrik/repo-sync", "jahrik", "repo-sync", false},
		{"git@github.com:bad", "", "", true},
		{"https://github.com/single", "", "", true},
	}

	for _, tc := range tests {
		owner, repo, err := parseOwnerRepo(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseOwnerRepo(%q) expected error, got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseOwnerRepo(%q) unexpected error: %v", tc.url, err)
			continue
		}
		if owner != tc.wantOwner || repo != tc.wantRepo {
			t.Errorf("parseOwnerRepo(%q) = (%q, %q), want (%q, %q)",
				tc.url, owner, repo, tc.wantOwner, tc.wantRepo)
		}
	}
}
