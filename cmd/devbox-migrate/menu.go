package main

import (
	"fmt"
	"io"
	"os"

	"devbox/internal/cliui"
	"devbox/internal/migration"
)

// Entering a review flow never approves copying or importing data.
func migrationMenu(ui *cliui.Runner, paths migration.Paths) (string, error) {
	stageReason, mergeReason, resumeReason := "", "not staged", "nothing pending"
	var stateErr error
	if _, err := os.Lstat(paths.Work); !os.IsNotExist(err) {
		var j *migration.Journal
		if err == nil {
			j, err = migration.Load(paths)
		}
		if err != nil {
			stateErr = err
			stageReason, mergeReason, resumeReason = "unreadable staging", "unreadable staging", "unreadable staging"
		} else {
			switch j.Phase {
			case "staging":
				stageReason, mergeReason, resumeReason = "copy incomplete", "copy incomplete", ""
			case "prepared":
				stageReason, mergeReason = "already staged", ""
			case "merging":
				stageReason, mergeReason, resumeReason = "merge incomplete", "merge incomplete", ""
			case "completed":
				stageReason, mergeReason, resumeReason = "completed", "completed", "completed"
			}
		}
	}
	selected := ""
	actions := []struct{ name, label, unavailable string }{
		{"preview", "Preview migration (read-only)", ""},
		{"stage", "Prepare staged copy", stageReason},
		{"merge", "Review and import", mergeReason},
		{"resume", "Resume", resumeReason},
	}
	err := ui.Run(func() (cliui.Screen, error) {
		page := cliui.Screen{Title: "Devbox -> Neo", Back: "Exit", Body: func(out io.Writer) error {
			_, err := fmt.Fprintf(out, "\nSource       %s\nDestination  %s\n", safe(paths.Source), safe(paths.Destination))
			return err
		}}
		for _, action := range actions {
			label, blocked := action.label, ""
			if action.unavailable != "" {
				label += " (" + action.unavailable + ")"
				blocked = "Unavailable: " + action.unavailable
				if stateErr != nil {
					blocked += fmt.Sprintf("\nCannot read migration state at %s: %s", safe(paths.Work), safe(stateErr.Error()))
				}
			}
			page.Actions = append(page.Actions, cliui.Action{Label: label, Blocked: blocked, Run: func() (bool, error) { selected = action.name; return true, nil }})
		}
		return page, nil
	})
	if err == nil && selected == "" {
		fmt.Fprintln(ui.Out, "Exited; no migration changes made.")
	}
	return selected, err
}
