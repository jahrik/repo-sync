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
	t.Setenv("PATH", t.TempDir()) // gh not on PATH
	cfg, err := Resolve("/tmp", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" {
		t.Errorf("expected empty token, got %q", cfg.Token)
	}
}

func TestResolveTokenFromGHCLI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", tmp) // no hosts.yml

	// Create a fake "gh" script that prints a token.
	script := filepath.Join(tmp, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho gh-cli-token\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp)

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "gh-cli-token" {
		t.Errorf("token = %q, want gh-cli-token", cfg.Token)
	}
}

func TestResolveLimit(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "flag-token")
	cfg, err := Resolve("/tmp", 42, "flag-token")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limit != 42 {
		t.Errorf("Limit = %d, want 42", cfg.Limit)
	}
}

func TestResolveDirExpanded(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Resolve("~/mydir", 1, "tok")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "mydir")
	if cfg.Dir != want {
		t.Errorf("Dir = %q, want %q", cfg.Dir, want)
	}
}

func TestTokenFromGHHostsHTTPS(t *testing.T) {
	tmp := t.TempDir()
	ghDir := filepath.Join(tmp, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	hostsYML := `github.com:
    oauth_token: https-token
    git_protocol: https
`
	if err := os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsYML), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "https-token" {
		t.Errorf("token = %q, want https-token", cfg.Token)
	}
	if cfg.UseSSH {
		t.Error("UseSSH should be false when git_protocol=https")
	}
}

func TestTokenFromGHHostsSSHCaseInsensitive(t *testing.T) {
	tmp := t.TempDir()
	ghDir := filepath.Join(tmp, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	hostsYML := `github.com:
    oauth_token: case-token
    git_protocol: SSH
`
	if err := os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsYML), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UseSSH {
		t.Error("UseSSH should be true for git_protocol=SSH (case-insensitive)")
	}
}

func TestTokenFromGHHostsMissingGithubCom(t *testing.T) {
	tmp := t.TempDir()
	ghDir := filepath.Join(tmp, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	// hosts.yml present but no github.com entry.
	hostsYML := `gitlab.com:
    oauth_token: other-token
    git_protocol: https
`
	if err := os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(hostsYML), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")
	stubGHAuthToken(t)

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	// Should succeed with no token (non-fatal).
	if cfg.Token != "" {
		t.Errorf("expected empty token when github.com missing from hosts.yml, got %q", cfg.Token)
	}
}

func TestTokenFromGHHostsBadYAML(t *testing.T) {
	tmp := t.TempDir()
	ghDir := filepath.Join(tmp, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghDir, "hosts.yml"), []byte(":::bad yaml:::"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("GITHUB_TOKEN", "")
	stubGHAuthToken(t)

	cfg, err := Resolve("/tmp", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	// Non-fatal: bad YAML is swallowed; token stays empty.
	if cfg.Token != "" {
		t.Errorf("expected empty token on bad YAML, got %q", cfg.Token)
	}
}

// stubGHAuthToken replaces the gh-CLI fallback with one that returns no token,
// so tests exercising the fallthrough don't invoke the real gh (which reads the
// system keyring and would leak the developer's credentials into test output).
func stubGHAuthToken(t *testing.T) {
	t.Helper()
	orig := ghAuthToken
	ghAuthToken = func() (string, error) { return "", nil }
	t.Cleanup(func() { ghAuthToken = orig })
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestLoadFileConfigNoFile(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	fc, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig with no file: %v", err)
	}
	if fc.Dir != nil || fc.Limit != nil || fc.Owner != nil {
		t.Error("expected all fields nil when no config file exists")
	}
}

func TestLoadFileConfigCWD(t *testing.T) {
	tmp := t.TempDir()
	chdir(t, tmp)
	t.Setenv("HOME", t.TempDir()) // no ~/.config/rs/config.yml

	yml := "dir: ~/mycode\nlimit: 50\nskip_forks: true\n"
	if err := os.WriteFile(filepath.Join(tmp, ".rs.yml"), []byte(yml), 0600); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if fc.Dir == nil || *fc.Dir != "~/mycode" {
		t.Errorf("Dir = %v, want ~/mycode", fc.Dir)
	}
	if fc.Limit == nil || *fc.Limit != 50 {
		t.Errorf("Limit = %v, want 50", fc.Limit)
	}
	if fc.SkipForks == nil || !*fc.SkipForks {
		t.Error("SkipForks should be true")
	}
}

func TestLoadFileConfigHomeDir(t *testing.T) {
	tmp := t.TempDir()
	chdir(t, t.TempDir()) // CWD has no .rs.yml
	t.Setenv("HOME", tmp)

	cfgDir := filepath.Join(tmp, ".config", "rs")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	yml := "owner: myorg\nformat: json\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(yml), 0600); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if fc.Owner == nil || *fc.Owner != "myorg" {
		t.Errorf("Owner = %v, want myorg", fc.Owner)
	}
	if fc.Format == nil || *fc.Format != "json" {
		t.Errorf("Format = %v, want json", fc.Format)
	}
}

func TestExpandHomeEdgeCases(t *testing.T) {
	tests := []struct {
		in string
	}{
		{""},
		{"/no/tilde"},
		{"no/tilde/relative"},
	}
	for _, tc := range tests {
		got, err := expandHome(tc.in)
		if err != nil {
			t.Errorf("expandHome(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.in {
			t.Errorf("expandHome(%q) = %q, want unchanged %q", tc.in, got, tc.in)
		}
	}
}
