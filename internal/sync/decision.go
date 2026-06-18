package sync

// Decide is a pure function that maps a DecisionInput to a RepoResult.
// It contains no I/O — it only applies the decision table.
func Decide(in DecisionInput) RepoResult {
	result := RepoResult{
		Branch: in.CurrentBranch,
		Ahead:  in.Ahead,
		Behind: in.Behind,
	}

	if in.IsOnDefault {
		switch {
		case in.IsDirty:
			result.Status = StatusDirty
		case in.WasBehind && in.DidPull:
			result.Status = StatusPulled
		case in.WasBehind:
			result.Status = StatusBehind
		default:
			result.Status = StatusOK
		}
		return result
	}

	// On a non-default branch.
	switch {
	case in.IsDirty:
		result.Status = StatusDirty

	case len(in.OpenPRs) > 0:
		pr := in.OpenPRs[0]
		result.Status = StatusOpenPR
		result.PRNumber = pr.GetNumber()
		result.PRTitle = pr.GetTitle()
		if in.Ahead > 0 {
			result.Ahead = in.Ahead
		}

	case in.Ahead == 0 && len(in.OpenPRs) == 0:
		// No commits ahead, no open PR → branch can be cleaned up.
		result.Status = StatusSynced

	case in.Ahead > 0 && len(in.OpenPRs) == 0 && len(in.MergedPRs) > 0:
		// All commits landed via a merged PR → clean up.
		result.Status = StatusSynced

	case in.Ahead > 0 && len(in.OpenPRs) == 0 && len(in.MergedPRs) == 0:
		result.Status = StatusUnmerged

	default:
		result.Status = StatusUnmerged
	}

	return result
}
