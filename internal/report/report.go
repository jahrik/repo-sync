package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jahrik/repo-sync/internal/sync"
)

var categoryOrder = map[sync.Status]int{
	sync.StatusCloned:   0,
	sync.StatusPulled:   1,
	sync.StatusBehind:   2,
	sync.StatusSynced:   3,
	sync.StatusOK:       4,
	sync.StatusOpenPR:   5,
	sync.StatusUnmerged: 6,
	sync.StatusDirty:    7,
	sync.StatusOrphaned: 8,
	sync.StatusError:    9,
}

var orderedStatuses = []sync.Status{
	sync.StatusCloned,
	sync.StatusPulled,
	sync.StatusBehind,
	sync.StatusSynced,
	sync.StatusOK,
	sync.StatusOpenPR,
	sync.StatusUnmerged,
	sync.StatusDirty,
	sync.StatusOrphaned,
	sync.StatusError,
}

const labelWidth = 10

// detailStyles holds per-element styles for the detail column.
type detailStyles struct {
	branch    lipgloss.Style // current branch name
	arrow     lipgloss.Style // → separator between branch and default
	def       lipgloss.Style // default branch name (when showing → main)
	count     lipgloss.Style // ↓N, +N, ↑N
	pr        lipgloss.Style // #42 PR title
	staleHdr  lipgloss.Style // "stale:"
	staleName lipgloss.Style // individual stale branch names
	errHdr    lipgloss.Style // "ERR:"
	errMsg    lipgloss.Style // error message text
	muted     lipgloss.Style // "no PR", "ahead", "uncommitted", "diverged"
	sep       lipgloss.Style // · separator
}

// Print writes a formatted report of sync results to w.
// Colors are enabled automatically when w is a TTY; plain text otherwise.
func Print(results []sync.RepoResult, w io.Writer) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(w, "No repositories processed.")
		return
	}

	rend := lipgloss.NewRenderer(w)

	// Palette.
	green := lipgloss.AdaptiveColor{Light: "2", Dark: "10"}
	yellow := lipgloss.AdaptiveColor{Light: "3", Dark: "11"}
	red := lipgloss.AdaptiveColor{Light: "1", Dark: "9"}
	cyan := lipgloss.AdaptiveColor{Light: "6", Dark: "14"}
	blue := lipgloss.AdaptiveColor{Light: "4", Dark: "12"}
	magenta := lipgloss.AdaptiveColor{Light: "5", Dark: "13"}
	muted := lipgloss.AdaptiveColor{Light: "243", Dark: "240"}

	// Status label styles: fixed-width, colored.
	base := rend.NewStyle().Width(labelWidth)
	labelFor := map[sync.Status]lipgloss.Style{
		sync.StatusOK:       base.Foreground(green),
		sync.StatusCloned:   base.Foreground(green).Bold(true),
		sync.StatusPulled:   base.Foreground(green).Bold(true),
		sync.StatusBehind:   base.Foreground(yellow),
		sync.StatusSynced:   base.Foreground(cyan),
		sync.StatusOpenPR:   base.Foreground(yellow).Bold(true),
		sync.StatusUnmerged: base.Foreground(red).Bold(true),
		sync.StatusDirty:    base.Foreground(red).Bold(true),
		sync.StatusOrphaned: base.Foreground(magenta),
		sync.StatusError:    base.Foreground(red).Bold(true),
	}

	ds := detailStyles{
		branch:    rend.NewStyle().Foreground(blue).Bold(true),
		arrow:     rend.NewStyle().Foreground(muted),
		def:       rend.NewStyle().Foreground(muted),
		count:     rend.NewStyle().Foreground(yellow).Bold(true),
		pr:        rend.NewStyle().Foreground(magenta),
		staleHdr:  rend.NewStyle().Foreground(yellow).Bold(true),
		staleName: rend.NewStyle().Foreground(yellow),
		errHdr:    rend.NewStyle().Foreground(red).Bold(true),
		errMsg:    rend.NewStyle().Foreground(red),
		muted:     rend.NewStyle().Foreground(muted),
		sep:       rend.NewStyle().Foreground(muted),
	}

	nameStyle := rend.NewStyle().Bold(true)
	warnHdr := rend.NewStyle().Bold(true)
	warnIcon := rend.NewStyle().Foreground(yellow).Bold(true).Render("!")

	// Sort by category, then alphabetically within each category.
	sorted := make([]sync.RepoResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		oi := categoryOrder[sorted[i].Status]
		oj := categoryOrder[sorted[j].Status]
		if oi != oj {
			return oi < oj
		}
		return sorted[i].Name < sorted[j].Name
	})

	counts := make(map[sync.Status]int)
	for _, r := range sorted {
		counts[r.Status]++
	}

	// Result lines.
	sep := ds.sep.Render("  ·  ")
	for _, r := range sorted {
		st, ok := labelFor[r.Status]
		if !ok {
			st = labelFor[sync.StatusError]
		}
		parts := buildDetailParts(r, ds)
		line := "  " + st.Render(string(r.Status)) + "  " + nameStyle.Render(r.Name)
		if len(parts) > 0 {
			line += "  " + strings.Join(parts, sep)
		}
		_, _ = fmt.Fprintln(w, line)
	}

	// Summary box.
	_, _ = fmt.Fprintln(w)
	var summaryParts []string
	summaryParts = append(summaryParts, fmt.Sprintf("%d repos", len(results)))
	for _, st := range orderedStatuses {
		if n := counts[st]; n > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%s %d", st, n))
		}
	}
	summaryBox := rend.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(muted).
		Padding(0, 1).
		MarginLeft(2)
	_, _ = fmt.Fprintln(w, summaryBox.Render(strings.Join(summaryParts, "  ·  ")))

	// Warnings.
	var warnings []string
	if n := counts[sync.StatusOpenPR]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have open pull requests", n))
	}
	if n := counts[sync.StatusUnmerged]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have unmerged commits with no PR", n))
	}
	if n := counts[sync.StatusDirty]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have uncommitted changes or diverged history", n))
	}
	if n := counts[sync.StatusOrphaned]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d local director(ies) have no matching GitHub repo", n))
	}
	if len(warnings) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, warnHdr.Render("Warnings:"))
		for _, msg := range warnings {
			_, _ = fmt.Fprintf(w, "  %s  %s\n", warnIcon, msg)
		}
	}
}

// buildDetailParts constructs styled detail segments for a result line.
func buildDetailParts(r sync.RepoResult, ds detailStyles) []string {
	var parts []string

	onFeature := r.Branch != "" && r.DefaultBranch != "" && r.Branch != r.DefaultBranch

	// Branch context.
	switch r.Status {
	case sync.StatusOK, sync.StatusCloned, sync.StatusOrphaned:
		// No branch detail for these.
	default:
		if r.Branch != "" {
			if onFeature {
				// feat/x → main
				b := ds.branch.Render(r.Branch) +
					ds.arrow.Render(" → ") +
					ds.def.Render(r.DefaultBranch)
				parts = append(parts, b)
			} else if r.Branch != "" {
				parts = append(parts, ds.branch.Render(r.Branch))
			}
		}
	}

	// Status-specific annotations.
	switch r.Status {
	case sync.StatusPulled, sync.StatusBehind:
		if r.Behind > 0 {
			parts = append(parts, ds.count.Render(fmt.Sprintf("↓%d", r.Behind)))
		}

	case sync.StatusOpenPR:
		parts = append(parts, ds.pr.Render(fmt.Sprintf("#%d %s", r.PRNumber, r.PRTitle)))
		if r.Ahead > 0 {
			parts = append(parts, ds.count.Render(fmt.Sprintf("+%d", r.Ahead)))
		}

	case sync.StatusUnmerged:
		if r.Ahead > 0 {
			parts = append(parts, ds.count.Render(fmt.Sprintf("+%d ahead", r.Ahead)))
		}
		parts = append(parts, ds.muted.Render("no PR"))

	case sync.StatusDirty:
		if r.Err != nil {
			// Pull failed — show diverged counts if available.
			if r.Ahead > 0 || r.Behind > 0 {
				parts = append(parts, ds.count.Render(fmt.Sprintf("↑%d ↓%d", r.Ahead, r.Behind)))
			}
			parts = append(parts, ds.muted.Render("diverged"))
		} else {
			parts = append(parts, ds.muted.Render("uncommitted"))
		}

	case sync.StatusError:
		if r.Err != nil {
			parts = append(parts,
				ds.errHdr.Render("ERR:")+ds.errMsg.Render(" "+r.Err.Error()))
		}
	}

	// Stale branches (shown for any status that has them).
	if len(r.StaleBranches) > 0 {
		names := make([]string, len(r.StaleBranches))
		for i, b := range r.StaleBranches {
			names[i] = ds.staleName.Render(b)
		}
		parts = append(parts,
			ds.staleHdr.Render("stale:")+ds.muted.Render(" ")+strings.Join(names, ds.muted.Render(", ")))
	}

	return parts
}

// jsonResult is the JSON-serializable form of a RepoResult.
type jsonResult struct {
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Branch        string   `json:"branch,omitempty"`
	DefaultBranch string   `json:"default_branch,omitempty"`
	PRNumber      int      `json:"pr_number,omitempty"`
	PRTitle       string   `json:"pr_title,omitempty"`
	Ahead         int      `json:"ahead,omitempty"`
	Behind        int      `json:"behind,omitempty"`
	StaleBranches []string `json:"stale_branches,omitempty"`
	Err           string   `json:"error,omitempty"`
}

// PrintJSON writes results as a JSON array to w.
func PrintJSON(results []sync.RepoResult, w io.Writer) error {
	out := make([]jsonResult, len(results))
	for i, r := range results {
		j := jsonResult{
			Name:          r.Name,
			Status:        string(r.Status),
			Branch:        r.Branch,
			DefaultBranch: r.DefaultBranch,
			PRNumber:      r.PRNumber,
			PRTitle:       r.PRTitle,
			Ahead:         r.Ahead,
			Behind:        r.Behind,
			StaleBranches: r.StaleBranches,
		}
		if r.Err != nil {
			j.Err = r.Err.Error()
		}
		out[i] = j
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
