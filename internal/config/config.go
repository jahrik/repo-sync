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
	Dir           string
	Limit         int
	Token         string
	UseSSH        bool
	Owner         string
	Fetch         bool     // fetch + prune existing repos, report status (no writes)
	Pull          bool     // fetch + fast-forward pull existing repos
	Checkout      bool     // switch SYNCED repos to the default branch and pull
	SkipForks     bool     // exclude forked repositories
	SkipArchived  bool     // exclude archived repositories
	ReportOrphans bool     // report local dirs with no matching GitHub repo
	Format        string   // output format: "text" (default) or "json"
	Filter        string   // regexp to match against repo name (empty = all)
	Ignore        []string // local directory names to exclude from orphan reports
}

// FileConfig holds values that can be set in a config file.
// All fields are pointers so we can distinguish "set" from "zero value".
type FileConfig struct {
	Dir           *string   `yaml:"dir"`
	Limit         *int      `yaml:"limit"`
	Token         *string   `yaml:"token"`
	Owner         *string   `yaml:"owner"`
	Pull          *bool     `yaml:"pull"`
	Fetch         *bool     `yaml:"fetch"`
	Checkout      *bool     `yaml:"checkout"`
	SkipForks     *bool     `yaml:"skip_forks"`
	SkipArchived  *bool     `yaml:"skip_archived"`
	ReportOrphans *bool     `yaml:"report_orphans"`
	Format        *string   `yaml:"format"`
	Filter        *string   `yaml:"filter"`
	Ignore        *[]string `yaml:"ignore"`
}

// LoadFileConfig reads the first config file found: .repo-sync.yml in the
// current directory, then ~/.config/repo-sync/config.yml. Returns an empty
// FileConfig (not an error) when no file exists.
func LoadFileConfig() (FileConfig, error) {
	var fc FileConfig

	candidates := []string{".repo-sync.yml"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "repo-sync", "config.yml"))
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fc, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return fc, fmt.Errorf("config: parse %s: %w", path, err)
		}
		return fc, nil
	}
	return fc, nil
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
