package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"devbox/internal/app"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

func statusCommand(factory engineFactory, profile *string) *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{Use: "status [target]", Short: "Show session details, active commands, and pending configuration changes", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if all && len(args) != 0 {
			return fmt.Errorf("--all does not accept an exact target")
		}
		if !all && len(args) != 1 {
			return fmt.Errorf("provide a target or --all")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if all {
			report, err := e.StatusAll(cmd.Context(), *profile)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			if len(report.Sessions) == 0 {
				cmd.Println("No matching saved environments.")
			} else if err := printStatusList(cmd.OutOrStdout(), report.Sessions); err != nil {
				return err
			}
			return printUnmatchedContainers(cmd.OutOrStdout(), report.UnmatchedContainers)
		}
		details, err := e.Status(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(details)
		}
		view := details.View
		printView(cmd, view)
		if details.Record != nil {
			cmd.Printf("Session: %s\nHarness: %s\nImage: %s\nActive commands: %d\n", displayCell(details.SessionID), displayCell(details.Harness), displayCell(details.Record.ImageID), len(details.Active))
		}
		cmd.Printf("Changes: %s\n", statusChange(view))
		if view.ConfigError != "" {
			cmd.Printf("Desired configuration error: %s\n", displayCell(view.ConfigError))
		}
		for _, change := range view.PendingInputChanges {
			cmd.Printf("  - [%s] %s\n", change.Scope, change)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print session details and status, or bulk inventory diagnostics, as JSON")
	cmd.Flags().BoolVar(&all, "all", false, "Check all saved environments, optionally limited by --profile NAME")
	return cmd
}

func statusChange(view app.View) string {
	if view.Error != "" || view.ConfigError != "" || view.Pending != nil {
		return "Cannot check"
	}
	switch view.Desired {
	case environment.NoChange:
		return "No changes"
	case environment.RuntimeSync:
		return "Runtime changes"
	case environment.Recreate:
		return "Recreate needed"
	case environment.RebuildAndRecreate:
		return "Rebuild + recreate needed"
	default:
		return "Cannot check"
	}
}

func printStatusList(out io.Writer, views []app.View) error {
	var table bytes.Buffer
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCONTAINER\tCHANGE")
	for _, view := range views {
		fmt.Fprintf(w, "%s\t%s\t%s\n", displayCell(view.Name), containerState(view), statusChange(view))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := printListRows(out, views, table.String()); err != nil {
		return err
	}
	for _, view := range views {
		if len(view.PendingInputChanges) > 0 {
			if _, err := fmt.Fprintf(out, "%s:\n", displayCell(view.Name)); err != nil {
				return err
			}
			for _, inputChange := range view.PendingInputChanges {
				if _, err := fmt.Fprintf(out, "  - [%s] %s\n", inputChange.Scope, inputChange); err != nil {
					return err
				}
			}
		}
		if view.ConfigError != "" {
			if _, err := fmt.Fprintf(out, "! %s: desired configuration: %s\n", displayCell(view.Name), displayCell(view.ConfigError)); err != nil {
				return err
			}
		}
		if view.Error == "" && view.ConfigError == "" && view.Pending == nil && (view.Desired == environment.Recreate || view.Desired == environment.RebuildAndRecreate) {
			if _, err := fmt.Fprintf(out, "  devbox-neo recreate %s\n", displayCell(view.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}
