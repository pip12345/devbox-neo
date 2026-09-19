package migration

import (
	"fmt"
	"sort"
	"strings"
)

func reportItemState(i Item, j *Journal) string {
	state := "Metadata OK"
	if i.Scanned {
		state = "Scanned"
	}
	if len(i.Warnings) > 0 {
		state = "Warning"
	}
	if len(i.Issues) > 0 {
		state = "Error"
	}
	if j != nil {
		if j.Phase == "prepared" {
			state = "Staged"
		}
		if j.Failure != "" && j.CurrentItem == i.Key {
			state = "Failed"
		}
		if j.Excluded[i.Key] != "" {
			state = "Skipped"
		}
		if j.Merge != nil {
			if j.Merge.Plan.Excluded[i.Key] != "" {
				state = "Skipped"
			} else if i.Kind == "session" {
				state = "Pending"
				if a := j.Merge.Attempts[i.Key]; a != nil && a.Phase == "done" {
					state = "Imported"
				}
			}
		}
	}
	return state
}

func inventorySummary(items []Item, j *Journal) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[reportItemState(item, j)]++
	}
	parts := []string{fmt.Sprintf("Items: %d", len(items))}
	for _, state := range []string{"Failed", "Error", "Warning", "Metadata OK", "Scanned", "Staged", "Pending", "Imported", "Skipped"} {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", state, counts[state]))
		}
	}
	return strings.Join(parts, " | ")
}

func reportGroup(kind string) string {
	switch kind {
	case "auth":
		return "Authentication"
	case "cache":
		return "Optional caches"
	case "global":
		return "Global configuration"
	case "profile":
		return "Profiles"
	case "project":
		return "Projects"
	case "session":
		return "Sessions"
	default:
		return display(kind)
	}
}

// Repeated notes have one number across the report, with their full text both
// beside each affected owner and in the summary. Item order fixes numbering;
// sorting an owner's numbers makes common notes easy to compare.
type reportNotes struct {
	shared  []string
	numbers map[string]int
}

func itemReportNotes(i Item) []string {
	var notes []string
	for _, n := range i.Notices {
		notes = append(notes, "Review: "+n)
	}
	for _, n := range i.Changes {
		notes = append(notes, "Conversion: "+n)
	}
	return notes
}

func collectReportNotes(items []Item) reportNotes {
	counts := map[string]int{}
	for _, i := range items {
		for _, n := range itemReportNotes(i) {
			counts[n]++
		}
	}
	notes := reportNotes{numbers: map[string]int{}}
	for _, i := range items {
		for _, n := range itemReportNotes(i) {
			if counts[n] > 1 && notes.numbers[n] == 0 {
				notes.shared = append(notes.shared, n)
				notes.numbers[n] = len(notes.shared)
			}
		}
	}
	return notes
}

func compactItem(b *strings.Builder, i Item, j *Journal, state string, notes reportNotes, paint reportPaint) {
	fmt.Fprintln(b, "  "+paint.itemLabel(i, state))
	if i.Workspace != "" {
		slot := "project"
		if i.Profile != "" {
			slot = "profile " + i.Profile
		}
		fmt.Fprintf(b, "    %s | %s\n", display(slot), display(i.Workspace))
	} else if i.Kind == "auth" {
		fmt.Fprintf(b, "    Path: %s\n", display(i.Path))
	}
	if j != nil {
		exclusion := j.Excluded[i.Key]
		if j.Merge != nil && j.Merge.Plan.Excluded[i.Key] != "" {
			exclusion = j.Merge.Plan.Excluded[i.Key]
		}
		if exclusion != "" {
			fmt.Fprintf(b, "    Exclusion: %s\n", display(exclusion))
		}
	}
	for _, issue := range i.Issues {
		fmt.Fprintf(b, "    Error: %s\n", display(issue))
	}
	for _, warning := range i.Warnings {
		fmt.Fprintf(b, "    Warning: %s\n", display(warning))
	}
	var numbers []int
	for _, n := range itemReportNotes(i) {
		if number := notes.numbers[n]; number > 0 {
			numbers = append(numbers, number)
		}
	}
	sort.Ints(numbers)
	for _, number := range numbers {
		fmt.Fprintf(b, "    %d. %s\n", number, display(notes.shared[number-1]))
	}
	for _, n := range itemReportNotes(i) {
		if notes.numbers[n] == 0 {
			fmt.Fprintf(b, "    %s\n", display(n))
		}
	}
	if i.Scanned {
		fmt.Fprintf(b, "    Portable bytes: %d\n", i.Bytes)
	}
}
