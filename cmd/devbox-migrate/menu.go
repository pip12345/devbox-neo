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
	fmt.Fprintf(ui.out, "Migrate Devbox -> Neo\n\nSource       %s\nStaging      %s\nDestination  %s\n\n", safe(paths.Source), safe(paths.Work), safe(paths.Destination))

	stageReason := "Staging already exists; it will not be replaced."
	mergeReason := "Copy old Devbox data into staging first."
	resumeReason := "There is no interrupted migration."
	if _, err := os.Lstat(paths.Work); os.IsNotExist(err) {
		stageReason = ""
		fmt.Fprintln(ui.out, "No import has been prepared yet. Start with option 1.")
	} else {
		var j *migration.Journal
		if err == nil {
			j, err = migration.Load(paths)
		}
		if err != nil {
			stageReason = "Cannot safely use the existing staging path; inspect it before continuing."
			mergeReason, resumeReason = stageReason, stageReason
			fmt.Fprintln(ui.out, "Cannot read migration state:", safe(err.Error()))
		} else {
			switch j.Phase {
			case "staging":
				mergeReason = "Finish copying into staging with option 3 first."
				resumeReason = ""
				fmt.Fprintln(ui.out, "Copying into staging is incomplete. Option 3 continues the approved copy.")
			case "prepared":
				mergeReason = ""
				resumeReason = "Staging is complete. Choose option 2 for separate import review and approval."
				fmt.Fprintln(ui.out, "Staged data is ready for review. Nothing has been imported into Neo yet.")
			case "merging":
				mergeReason = "An import is already approved and incomplete. Continue it with option 3."
				resumeReason = ""
				fmt.Fprintln(ui.out, "Import into Neo is incomplete. Option 3 continues previously approved work.")
			case "completed":
				mergeReason = "This migration is already complete."
				fmt.Fprintln(ui.out, "Migration is complete. Keep the originals, staging data, and backups until you choose otherwise.")
			}
			fmt.Fprintf(ui.out, "Report: %s/report.txt\n", safe(paths.Work))
		}
	}

	actions := []struct {
		name, label, explanation, unavailable string
	}{
		{"stage", "Copy old Devbox data into staging", "Copy and convert data into a separate folder. Neither installation nor project files are changed.", stageReason},
		{"merge", "Review and import staged data into Neo", "Review conflicts and approve changes before importing data and building new Neo containers.", mergeReason},
		{"resume", "Continue an interrupted migration", "Continue previously approved work without resetting completed imports. This can make changes immediately.", resumeReason},
	}
	for {
		if err := cmd.Context().Err(); err != nil {
			return "", err
		}
		fmt.Fprintln(ui.out, "\nWhat would you like to do?")
		for n, action := range actions {
			ui.option(n+1, action.label)
			fmt.Fprintln(ui.out, "        "+action.explanation)
			if action.unavailable != "" {
				fmt.Fprintln(ui.out, "        Unavailable:", action.unavailable)
			}
		}
		choice, err := ui.readChoice(len(actions), "Exit")
		if err != nil {
			return "", err
		}
		switch choice {
		case "1", "2", "3":
			action := actions[int(choice[0]-'1')]
			if action.unavailable != "" {
				fmt.Fprintln(ui.out, "Unavailable:", action.unavailable)
				continue
			}
			return action.name, nil
		case "0":
			fmt.Fprintln(ui.out, "Exited; no migration changes made.")
			return "", nil
		}
	}
}
