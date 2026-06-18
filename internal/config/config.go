package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds resolved runtime configuration.
type Config struct {
	Dir    string
	Limit  int
	Token  string
	UseSSH bool
}

// hostsEntry represents one entry under github.com in gh's hosts.yml.
type hostsEntry struct {
	OAuthToken  string `yaml:"oauth_token"`
	GitProtocol string `yaml:"git_protocol"`
}

// Resolve builds a Config from flag values, environment variables, and the gh
// CLI hosts.yml file.  Priority for token: flag → GITHUB_TOKEN env → hosts.yml.
func Resolve(dir string, limit int, token string) (Config, error) {
	cfg := Config{Limit: limit}

	// Expand dir.
	expanded, err := expandHome(dir)
	if err != nil {
		return cfg, fmt.Errorf("config: expand dir: %w", err)
	}
	cfg.Dir = expanded

	// Token resolution.
	if token != "" {
		cfg.Token = token
	} else if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		cfg.Token = t
	} else {
		t, useSSH, err := tokenFromGHHosts()
		if err != nil {
			// Non-fatal; we'll attempt unauthenticated calls (rate-limited).
			_ = err
		} else {
			cfg.Token = t
			cfg.UseSSH = useSSH
		}
	}

	return cfg, nil
}

// tokenFromGHHosts reads ~/.config/gh/hosts.yml and returns the token and
// whether git_protocol == "ssh" for github.com.
func tokenFromGHHosts() (token string, useSSH bool, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(home, ".config", "gh", "hosts.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}

	var hosts map[string]hostsEntry
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return "", false, fmt.Errorf("config: parse hosts.yml: %w", err)
	}

	entry, ok := hosts["github.com"]
	if !ok {
		return "", false, fmt.Errorf("config: github.com not found in hosts.yml")
	}

	return entry.OAuthToken, strings.EqualFold(entry.GitProtocol, "ssh"), nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}
