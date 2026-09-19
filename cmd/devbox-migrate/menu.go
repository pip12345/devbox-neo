package main

import (
	"bufio"
	"fmt"
	"os"

	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

// Choosing an action enters its existing review flow; it is not approval to
// copy or import anything. Resume alone continues previously approved work.
func migrationMenu(cmd *cobra.Command, paths migration.Paths) (string, error) {
	ui := menu{in: bufio.NewReader(cmd.InOrStdin()), out: cmd.OutOrStdout()}
	fmt.Fprintf(ui.out, "Devbox -> Neo\n\nSource       %s\nDestination  %s\n", safe(paths.Source), safe(paths.Destination))

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

	actions := []struct {
		name, label, unavailable string
	}{
		{"preview", "Preview migration (read-only)", ""},
		{"stage", "Prepare staged copy", stageReason},
		{"merge", "Review and import", mergeReason},
		{"resume", "Resume", resumeReason},
	}
	for {
		if err := cmd.Context().Err(); err != nil {
			return "", err
		}
		fmt.Fprintln(ui.out)
		for n, action := range actions {
			label := action.label
			if action.unavailable != "" {
				label += " (" + action.unavailable + ")"
			}
			ui.option(n+1, label)
		}
		choice, err := ui.readChoice(len(actions), "Exit")
		if err != nil {
			return "", err
		}
		if choice == "0" {
			fmt.Fprintln(ui.out, "Exited; no migration changes made.")
			return "", nil
		}
		action := actions[int(choice[0]-'1')]
		if action.unavailable != "" {
			fmt.Fprintln(ui.out, "Unavailable:", action.unavailable)
			if stateErr != nil {
				fmt.Fprintf(ui.out, "Cannot read migration state at %s: %s\n", safe(paths.Work), safe(stateErr.Error()))
			}
			continue
		}
		return action.name, nil
	}
}
