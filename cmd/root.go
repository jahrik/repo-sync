package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
	githubclient "github.com/jahrik/repo-sync/internal/github"
	"github.com/jahrik/repo-sync/internal/report"
	"github.com/jahrik/repo-sync/internal/sync"
	"github.com/spf13/cobra"
)

var (
	flagDir   string
	flagLimit int
	flagToken string
	flagOwner string
)

var rootCmd = &cobra.Command{
	Use:   "repo-sync",
	Short: "Sync all your GitHub repositories",
	Long: `repo-sync clones missing repositories, fast-forwards default branches,
cleans up merged feature branches, and reports on open PRs and unmerged work.`,
	SilenceUsage: true,
	RunE:         run,
}

// SetVersion wires build-time version information into the root command so
// that "repo-sync --version" prints meaningful output.  Called from main.
func SetVersion(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDir, "dir", "~/github", "directory containing your local clones")
	rootCmd.PersistentFlags().IntVar(&flagLimit, "limit", 200, "maximum number of repositories to process")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "GitHub personal access token (overrides GITHUB_TOKEN env and gh CLI config)")
	rootCmd.PersistentFlags().StringVar(&flagOwner, "owner", "", "GitHub user or org to sync (default: authenticated user)")
}

func run(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Resolve(flagDir, flagLimit, flagToken)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	gh, err := githubclient.NewClient(cfg.Token)
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	if flagOwner != "" {
		cfg.Owner = flagOwner
	} else {
		cfg.Owner = gh.Owner()
	}

	gitRunner := git.NewRunner()

	results, err := sync.Run(ctx, cfg, gh, gitRunner, cfg.Dir)
	if err != nil {
		// Print partial results before returning the error.
		if len(results) > 0 {
			report.Print(results, os.Stdout)
		}
		return fmt.Errorf("sync: %w", err)
	}

	report.Print(results, os.Stdout)
	return nil
}
