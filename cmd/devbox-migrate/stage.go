package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/cliui"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func stageMenu(ui *cliui.Runner, cmd *cobra.Command, runtime migration.SourceRuntime, v *migration.Inventory, s migration.Selection) error {
	completed := false
	err := ui.Run(func() (cliui.Screen, error) {
		return cliui.Screen{Title: "Migrate Devbox -> Neo", Back: "Cancel", Prompt: "What would you like to do?", Body: func(out io.Writer) error {
			fmt.Fprintf(out, "\nSource       %s\nStaging      %s\nDestination  %s\n\n", safe(v.Paths.Source), safe(v.Paths.Work), safe(v.Paths.Destination))
			if _, err := os.Lstat(v.Paths.Destination); err == nil {
				fmt.Fprintln(out, "Destination already exists; its contents will not be changed.")
			}
			fmt.Fprintln(out, "This step stages host-backed files only. Merge requires a separate explicit approval.")
			_, err := fmt.Fprintln(out, "Existing Neo data and project files stay untouched. Keep the old CLI and source containers idle during copying.")
			return err
		}, Actions: []cliui.Action{
			{Label: "Review inventory and issues", Run: func() (bool, error) {
				return false, ui.View("Inventory and issues", func(out io.Writer) error {
					detailed, err := cmd.Flags().GetBool("verbose")
					if err != nil {
						return err
					}
					if detailed {
						return migration.Report(out, v, nil)
					}
					return migration.CompactReport(out, v, nil)
				})
			}},
			{Label: "Choose what to stage", Run: func() (bool, error) { return false, choose(ui, v, &s) }},
			{Label: "Prepare supported items (review exclusions first)", Run: func() (bool, error) {
				done, err := prepareStage(ui, cmd, runtime, v, s)
				completed = done
				return done, err
			}},
			{Label: "Rescan after manual fixes", Run: func() (bool, error) {
				next, err := migration.InventorySource(cmd.Context(), v.Paths)
				if err != nil {
					ui.Notice("Cannot rescan: " + safe(err.Error()))
					return false, nil
				}
				v = next
				kept := []string{}
				for _, key := range s.Skip {
					for _, item := range v.Items {
						if item.Key == key {
							kept = append(kept, key)
						}
					}
				}
				s.Skip = kept
				ui.Notice("Inventory refreshed; no destination or project changes.")
				return false, nil
			}},
		}}, nil
	})
	if err == nil && !completed {
		fmt.Fprintln(ui.Out, "Cancelled.")
	}
	return err
}

func prepareStage(ui *cliui.Runner, cmd *cobra.Command, runtime migration.SourceRuntime, v *migration.Inventory, s migration.Selection) (bool, error) {
	proposed := s
	proposed.Skip = slices.Clone(s.Skip)
	proposed.ExternalAuth = slices.Clone(s.ExternalAuth)
	for _, item := range v.Items {
		if len(item.Issues) > 0 && !has(proposed.Skip, item.Key) {
			proposed.Skip = append(proposed.Skip, item.Key)
		}
	}
	// This approval permits copying into private staging, never replacing auth.
	for _, item := range v.Items {
		if item.Kind != "auth" || has(proposed.Skip, item.Key) || has(proposed.ExternalAuth, item.Key) {
			continue
		}
		if item.Path == v.Paths.Source || strings.HasPrefix(item.Path, v.Paths.Source+string(filepath.Separator)) {
			continue
		}
		confirmed, err := ui.Confirm("Copy external " + safe(item.Key) + " from " + safe(item.Path) + "? [y/N] ")
		if err != nil {
			return false, err
		}
		if confirmed {
			proposed.ExternalAuth = append(proposed.ExternalAuth, item.Key)
		} else {
			proposed.Skip = append(proposed.Skip, item.Key)
		}
	}
	excluded, err := v.Select(proposed)
	fmt.Fprintln(ui.Out, "\nStaging scope:")
	selected := 0
	for _, item := range v.Items {
		if reason := excluded[item.Key]; reason != "" {
			fmt.Fprintf(ui.Out, "  Skip %s (%s)\n", safe(item.Key), safe(reason))
		} else {
			fmt.Fprintf(ui.Out, "  Copy %s\n", safe(item.Key))
			for _, warning := range item.Warnings {
				fmt.Fprintf(ui.Out, "    Warning: %s\n", safe(warning))
			}
			selected++
		}
	}
	if err != nil {
		ui.Notice("Review required: " + safe(err.Error()))
		return false, nil
	}
	fmt.Fprintf(ui.Out, "\nSelected items: %d\nCaches selected: %t\n", selected, proposed.Caches)
	fmt.Fprintln(ui.Out, "Data sizes and deep-tree checks are deferred until approval. Only selected data will be scanned and copied.")
	fmt.Fprintln(ui.Out, "Project conversions are staged proposals, not live edits. Review container-only config capture and destination conflicts with --merge.")
	confirmed, err := ui.Confirm("Prepare this scope, including the listed exclusions? [y/N] ")
	if err != nil || !confirmed {
		return false, err
	}
	for {
		if err := ui.Pause(); err != nil {
			return false, err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Checking stopped source writers, then copying and verifying host-backed data...")
		j, stageErr := migration.Stage(cmd.Context(), v, proposed, runtime, cmd.OutOrStdout())
		if j != nil {
			return true, errors.Join(stageErr, printInventory(cmd, &j.Inventory, j))
		}
		if stageErr == nil {
			return true, nil
		}
		ui.Notice("Cannot stage: " + safe(stageErr.Error()))
		choice, err := ui.Select("What would you like to do?", []string{"Recheck", "Cancel migration"}, "Back")
		if err != nil {
			return false, err
		}
		if choice == 0 {
			continue
		}
		if choice == 1 {
			fmt.Fprintln(ui.Out, "Cancelled.")
			return true, nil
		}
		return false, nil
	}
}

func has(values []string, value string) bool { return slices.Contains(values, value) }
func choose(ui *cliui.Runner, v *migration.Inventory, s *migration.Selection) error {
	return ui.Run(func() (cliui.Screen, error) {
		page := cliui.Screen{Title: "Toggle an item by number. Skipping an owner also skips dependent sessions.", Back: "Back", Rows: func(out io.Writer, actions []cliui.Action) error {
			// Inventory labels already carry status styling. Keep those rows
			// intact; oversized frames use the shared scrolling renderer.
			for i, action := range actions {
				if _, err := fmt.Fprintf(out, "%s%s\n", cliui.Prefix(i+1), action.Label); err != nil {
					return err
				}
			}
			return nil
		}}
		for _, item := range v.Items {
			state := "include"
			if has(s.Skip, item.Key) {
				state = "skip"
			}
			page.Actions = append(page.Actions, cliui.Action{Label: fmt.Sprintf("[%s] %s", state, migration.InventoryItemLabel(ui.Out, item)), Run: func() (bool, error) {
				if has(s.Skip, item.Key) {
					s.Skip = without(s.Skip, item.Key)
				} else {
					s.Skip = append(s.Skip, item.Key)
				}
				return false, nil
			}})
		}
		page.Actions = append(page.Actions,
			cliui.Action{Label: fmt.Sprintf("Toggle caches (currently %t)", s.Caches), Run: func() (bool, error) { s.Caches = !s.Caches; return false, nil }},
			cliui.Action{Label: "Exclude all items", Run: func() (bool, error) {
				s.Skip = nil
				for _, item := range v.Items {
					s.Skip = append(s.Skip, item.Key)
				}
				s.Caches = false
				return false, nil
			}},
		)
		return page, nil
	})
}
