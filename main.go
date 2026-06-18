package main

import "github.com/jahrik/repo-sync/cmd"

// version, commit, and date are set at build time by GoReleaser via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersion(version, commit, date)
	cmd.Execute()
}
