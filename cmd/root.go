package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jahrik/repo-sync/internal/config"
	"github.com/jahrik/repo-sync/internal/git"
	githubclient "github.com/jahrik/repo-sync/internal/github"
	"github.com/jahrik/repo-sync/internal/report"
	reposync "github.com/jahrik/repo-sync/internal/sync"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	flagDir           string
	flagLimit         int
	flagToken         string
	flagOwner         string
	flagFetch         bool
	flagPull          bool
	flagSkipForks     bool
	flagSkipArchived  bool
	flagReportOrphans bool
	flagFormat        string
	flagFilter        string
)

var rootCmd = &cobra.Command{
	Use:   "repo-sync",
	Short: "Sync all your GitHub repositories",
	Long: `repo-sync clones any repositories you don't have locally yet, then
optionally pulls updates and cleans up merged branches.

Default (no flags): clone missing repos and report.
  --fetch: also fetch existing repos and report their status (no writes).
  --pull : also fast-forward pull existing repos after fetching.`,
	SilenceUsage: true,
	RunE:         run,
}

// SetVersion wires build-time version information into the root command.
func SetVersion(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// exitError wraps an error with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		code := 1
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
		}
		os.Exit(code)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDir, "dir", "~/github", "directory containing your local clones")
	rootCmd.PersistentFlags().IntVar(&flagLimit, "limit", 200, "maximum number of repositories to process")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "GitHub personal access token (overrides GITHUB_TOKEN env and gh CLI config)")
	rootCmd.PersistentFlags().StringVar(&flagOwner, "owner", "", "GitHub user or org to sync (default: authenticated user)")
	rootCmd.PersistentFlags().BoolVar(&flagFetch, "fetch", false, "fetch and report status of existing repos (no writes)")
	rootCmd.PersistentFlags().BoolVar(&flagPull, "pull", false, "fetch and fast-forward pull existing repos (implies --fetch)")
	rootCmd.PersistentFlags().BoolVar(&flagSkipForks, "skip-forks", false, "exclude forked repositories")
	rootCmd.PersistentFlags().BoolVar(&flagSkipArchived, "skip-archived", false, "exclude archived repositories")
	rootCmd.PersistentFlags().BoolVar(&flagReportOrphans, "report-orphans", false, "report local directories that have no matching GitHub repo")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", "text", "output format: text or json")
	rootCmd.PersistentFlags().StringVar(&flagFilter, "filter", "", "regexp to filter repos by name (empty = all)")
}

func run(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load file config; apply values only for flags not explicitly set by the user.
	fc, fcErr := config.LoadFileConfig()
	if fcErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config file: %v\n", fcErr)
	}
	flags := cmd.Flags()
	applyFileString := func(name string, val *string) {
		if val != nil && !flags.Changed(name) {
			_ = flags.Set(name, *val)
		}
	}
	applyFileInt := func(name string, val *int) {
		if val != nil && !flags.Changed(name) {
			_ = flags.Set(name, fmt.Sprintf("%d", *val))
		}
	}
	applyFileBool := func(name string, val *bool) {
		if val != nil && !flags.Changed(name) {
			if *val {
				_ = flags.Set(name, "true")
			}
		}
	}
	applyFileString("dir", fc.Dir)
	applyFileInt("limit", fc.Limit)
	applyFileString("token", fc.Token)
	applyFileString("owner", fc.Owner)
	applyFileBool("pull", fc.Pull)
	applyFileBool("fetch", fc.Fetch)
	applyFileBool("skip-forks", fc.SkipForks)
	applyFileBool("skip-archived", fc.SkipArchived)
	applyFileBool("report-orphans", fc.ReportOrphans)
	applyFileString("format", fc.Format)
	applyFileString("filter", fc.Filter)

	cfg, err := config.Resolve(flagDir, flagLimit, flagToken)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	cfg.Fetch = flagFetch || flagPull
	cfg.Pull = flagPull
	cfg.SkipForks = flagSkipForks
	cfg.SkipArchived = flagSkipArchived
	cfg.ReportOrphans = flagReportOrphans
	cfg.Format = flagFormat
	cfg.Filter = flagFilter

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

	// Stderr renderer for live progress messages.
	srend := lipgloss.NewRenderer(os.Stderr)
	clonedStyle := srend.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"}).Bold(true)
	errorStyle := srend.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"}).Bold(true)
	dimStyle := srend.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"})

	fmt.Fprintf(os.Stderr, "%s\n", dimStyle.Render(fmt.Sprintf("Fetching repository list for %s...", cfg.Owner)))

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

		var parts []string
		if c > 0 {
			parts = append(parts, fmt.Sprintf("%d to clone", c))
		}
		if e > 0 {
			if cfg.Fetch || cfg.Pull {
				parts = append(parts, fmt.Sprintf("%d to sync", e))
			} else {
				parts = append(parts, fmt.Sprintf("%d already present", e))
			}
		}

		label := fmt.Sprintf("Found %d repos", t)
		if len(parts) > 0 {
			label += " (" + strings.Join(parts, ", ") + ")"
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
			msg = "  " + clonedStyle.Render("cloned") + "  " + r.Name
		case reposync.StatusError:
			msg = "  " + errorStyle.Render("error") + "   " + r.Name + ": " + fmt.Sprintf("%v", r.Err)
		}

		if bar != nil {
			if msg != "" {
				_ = bar.Clear()
				fmt.Fprintln(os.Stderr, msg)
			}
			if r.Status != reposync.StatusOrphaned {
				bar.Describe(r.Name)
				_ = bar.Add(1)
			}
		} else if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	results, err := reposync.Run(ctx, cfg, gh, gitRunner, cfg.Dir, onStart, onResult)
	if err != nil {
		if len(results) > 0 {
			if cfg.Format == "json" {
				_ = report.PrintJSON(results, os.Stdout)
			} else {
				report.Print(results, os.Stdout)
			}
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
	if total == 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n", dimStyle.Render("No repositories matched the current filters."))
	} else if cfg.Fetch || cfg.Pull {
		fmt.Fprintf(os.Stderr, "%s\n\n", dimStyle.Render(fmt.Sprintf("Done — processed %d repos.", total)))
	} else if clones > 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n", dimStyle.Render(fmt.Sprintf("Done — cloned %d new repo(s).", clones)))
	} else if existing > 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n", dimStyle.Render(fmt.Sprintf("All %d repos already present. Use --pull to update them.", existing)))
	}

	if cfg.Format == "json" {
		if err := report.PrintJSON(results, os.Stdout); err != nil {
			return err
		}
	} else {
		report.Print(results, os.Stdout)
	}

	return exitCodeFor(results)
}

// exitCodeFor returns nil (exit 0) when everything is clean, or an exitError
// with a specific code when attention is needed:
//
//	2 = dirty working trees or unmerged branches present
//	3 = open pull requests present
//	1 = hard error (sync failure, returned earlier)
func exitCodeFor(results []reposync.RepoResult) error {
	hasError := false
	hasDirtyOrUnmerged := false
	hasOpenPR := false

	for _, r := range results {
		switch r.Status {
		case reposync.StatusError:
			hasError = true
		case reposync.StatusDirty, reposync.StatusUnmerged:
			hasDirtyOrUnmerged = true
		case reposync.StatusOpenPR:
			hasOpenPR = true
		}
	}

	switch {
	case hasError:
		return &exitError{code: 1, err: fmt.Errorf("one or more repos had errors")}
	case hasDirtyOrUnmerged:
		return &exitError{code: 2, err: fmt.Errorf("one or more repos have dirty or unmerged work")}
	case hasOpenPR:
		return &exitError{code: 3, err: fmt.Errorf("one or more repos have open pull requests")}
	}
	return nil
}
