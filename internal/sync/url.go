package sync

import (
	"fmt"
	"net/url"
	"strings"
)

// parseOwnerRepo parses a GitHub remote URL (SSH or HTTPS) into owner and repo.
func parseOwnerRepo(remoteURL string) (owner, repo string, err error) {
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	if strings.HasPrefix(remoteURL, "git@github.com:") {
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("parseOwnerRepo: invalid SSH URL %q", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	u, parseErr := url.Parse(remoteURL)
	if parseErr != nil {
		return "", "", fmt.Errorf("parseOwnerRepo: %w", parseErr)
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("parseOwnerRepo: invalid HTTPS URL %q", remoteURL)
	}
	return parts[0], parts[1], nil
}
