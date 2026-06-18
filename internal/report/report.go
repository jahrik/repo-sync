package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jahrik/repo-sync/internal/sync"
)

// categoryOrder defines the display order for status categories.
var categoryOrder = map[sync.Status]int{
	sync.StatusCloned:   0,
	sync.StatusBehind:   1,
	sync.StatusCleaned:  2,
	sync.StatusOK:       3,
	sync.StatusOpenPR:   4,
	sync.StatusUnmerged: 5,
	sync.StatusDirty:    6,
	sync.StatusError:    7,
}

// Print writes a formatted report of sync results to w.
func Print(results []sync.RepoResult, w io.Writer) {
	if len(results) == 0 {
		fmt.Fprintln(w, "No repositories processed.")
		return
	}

	// Sort by category order, then alphabetically within each category.
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

	// Count by status.
	counts := make(map[sync.Status]int)
	for _, r := range sorted {
		counts[r.Status]++
	}

	// Print each result.
	for _, r := range sorted {
		line := formatResult(r)
		fmt.Fprintln(w, line)
	}

	// Summary line.
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d repos", len(results))
	orderedStatuses := []sync.Status{
		sync.StatusCloned,
		sync.StatusBehind,
		sync.StatusCleaned,
		sync.StatusOK,
		sync.StatusOpenPR,
		sync.StatusUnmerged,
		sync.StatusDirty,
		sync.StatusError,
	}
	for _, s := range orderedStatuses {
		if n := counts[s]; n > 0 {
			fmt.Fprintf(w, " | %s: %d", s, n)
		}
	}
	fmt.Fprintln(w)

	// Warnings.
	var warnings []string
	if n := counts[sync.StatusOpenPR]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have open pull requests", n))
	}
	if n := counts[sync.StatusUnmerged]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have unmerged commits with no PR", n))
	}
	if n := counts[sync.StatusDirty]; n > 0 {
		warnings = append(warnings, fmt.Sprintf("%d repo(s) have uncommitted changes", n))
	}
	if len(warnings) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Warnings:")
		for _, msg := range warnings {
			fmt.Fprintf(w, "  ! %s\n", msg)
		}
	}
}

func formatResult(r sync.RepoResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %-12s  %s", r.Status, r.Name))

	switch r.Status {
	case sync.StatusOpenPR:
		sb.WriteString(fmt.Sprintf("  [#%d: %s]", r.PRNumber, r.PRTitle))
		if r.Ahead > 0 {
			sb.WriteString(fmt.Sprintf(" (+%d)", r.Ahead))
		}
	case sync.StatusBehind:
		if r.Behind > 0 {
			sb.WriteString(fmt.Sprintf(" (↓%d)", r.Behind))
		}
	case sync.StatusUnmerged:
		if r.Ahead > 0 {
			sb.WriteString(fmt.Sprintf(" (+%d ahead, no PR)", r.Ahead))
		}
	case sync.StatusError:
		if r.Err != nil {
			sb.WriteString(fmt.Sprintf("  ERR: %v", r.Err))
		}
	}

	if r.Branch != "" && r.Status != sync.StatusOK && r.Status != sync.StatusCloned {
		sb.WriteString(fmt.Sprintf("  [branch: %s]", r.Branch))
	}

	return sb.String()
}
