package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		in   string
		want string
	}{
		{"~/github", filepath.Join(home, "github")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tc := range tests {
		got, err := expandHome(tc.in)
		if err != nil {
			t.Fatalf("expandHome(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTokenPriority(t *testing.T) {
	// Token from flag wins over env.
	t.Setenv("GITHUB_TOKEN", "env-token")
	cfg, err := Resolve("/tmp", 10, "flag-token")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "flag-token" {
		t.Errorf("token = %q, want flag-token", cfg.Token)
	}
}

func TestResolveTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "env-token" {
		t.Errorf("token = %q, want env-token", cfg.Token)
	}
}

func TestResolveTokenFromHostsYML(t *testing.T) {
	tmp := t.TempDir()
	ghDir := filepath.Join(tmp, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	hostsYML := `github.com:
    oauth_token: hosts-token
    git_protocol: ssh
`
	if err := os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsYML), 0600); err != nil {
		t.Fatal(err)
	}

	// Override UserHomeDir by patching HOME.
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "hosts-token" {
		t.Errorf("token = %q, want hosts-token", cfg.Token)
	}
	if !cfg.UseSSH {
		t.Error("UseSSH should be true when git_protocol=ssh")
	}
}

func TestResolveNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // no hosts.yml there
	cfg, err := Resolve("/tmp", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	// Should succeed with empty token (unauthenticated mode).
	if cfg.Token != "" {
		t.Errorf("expected empty token, got %q", cfg.Token)
	}
}
