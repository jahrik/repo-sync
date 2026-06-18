package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
	githubclient "github.com/jahrik/repo-sync/internal/github"
	"github.com/jahrik/repo-sync/internal/report"
	reposync "github.com/jahrik/repo-sync/internal/sync"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	flagDir   string
	flagLimit int
	flagToken string
	flagOwner string
	flagPull  bool
	flagClean bool
)

var rootCmd = &cobra.Command{
	Use:   "repo-sync",
	Short: "Sync all your GitHub repositories",
	Long: `repo-sync clones any repositories you don't have locally yet, then
optionally pulls updates and cleans up merged branches.

Default (no flags): clone missing repos and report.
  --pull : also fetch and fast-forward-pull existing repos.
  --clean: also switch to the default branch when the current branch is merged.`,
	SilenceUsage: true,
	RunE:         run,
}

// SetVersion wires build-time version information into the root command.
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
	rootCmd.PersistentFlags().BoolVar(&flagPull, "pull", false, "fetch and fast-forward-pull existing repos")
	rootCmd.PersistentFlags().BoolVar(&flagClean, "clean", false, "switch to default branch when current branch is merged (implies --pull)")
}

func run(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Resolve(flagDir, flagLimit, flagToken)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	cfg.Pull = flagPull || flagClean
	cfg.Clean = flagClean

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

	fmt.Fprintf(os.Stderr, "Fetching repository list for %s...\n", cfg.Owner)

	var (
		mu       sync.Mutex
		bar      *progressbar.ProgressBar
		total    int
		clones   int
		existing int
	)

	onStart := func(t, c, e int) {
		mu.Lock()
		defer mu.Unlock()
		total, clones, existing = t, c, e

		parts := []string{}
		if c > 0 {
			parts = append(parts, fmt.Sprintf("%d to clone", c))
		}
		if e > 0 {
			if cfg.Pull || cfg.Clean {
				parts = append(parts, fmt.Sprintf("%d to sync", e))
			} else {
				parts = append(parts, fmt.Sprintf("%d already present", e))
			}
		}

		label := fmt.Sprintf("Found %d repos", t)
		if len(parts) > 0 {
			label += " ("
			for i, p := range parts {
				if i > 0 {
					label += ", "
				}
				label += p
			}
			label += ")"
		}
		fmt.Fprintln(os.Stderr, label)

		if t == 0 {
			return
		}

		bar = progressbar.NewOptions(t,
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionSetWidth(45),
			progressbar.OptionThrottle(50*time.Millisecond),
			progressbar.OptionShowCount(),
			progressbar.OptionSetDescription("Starting..."),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintln(os.Stderr)
			}),
		)
	}

	onResult := func(r reposync.RepoResult) {
		mu.Lock()
		defer mu.Unlock()

		// Print a human-friendly line for notable events, clearing the bar first
		// so the message appears above it.
		var msg string
		switch r.Status {
		case reposync.StatusCloned:
			msg = fmt.Sprintf("  cloned  %s", r.Name)
		case reposync.StatusError:
			msg = fmt.Sprintf("  error   %s: %v", r.Name, r.Err)
		}

		if bar != nil {
			if msg != "" {
				_ = bar.Clear()
				fmt.Fprintln(os.Stderr, msg)
			}
			bar.Describe(r.Name)
			_ = bar.Add(1)
		} else if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	results, err := reposync.Run(ctx, cfg, gh, gitRunner, cfg.Dir, onStart, onResult)
	if err != nil {
		if len(results) > 0 {
			report.Print(results, os.Stdout)
		}
		return fmt.Errorf("sync: %w", err)
	}

	// Ensure the progress bar line is cleared before the report.
	mu.Lock()
	if bar != nil {
		_ = bar.Finish()
	}
	mu.Unlock()

	if total > 0 {
		fmt.Fprintln(os.Stderr)
	}

	// Summarise what happened before the detailed report.
	if cfg.Pull || cfg.Clean {
		fmt.Fprintf(os.Stderr, "Done — processed %d/%d repos.\n\n", len(results), total)
	} else if clones > 0 {
		fmt.Fprintf(os.Stderr, "Done — cloned %d new repo(s).\n\n", clones)
	} else if existing > 0 {
		fmt.Fprintf(os.Stderr, "All %d repos already present. Use --pull to update them.\n\n", existing)
	}

	report.Print(results, os.Stdout)
	return nil
}
