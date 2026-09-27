package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func statusCommand(factory engineFactory, localName *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "status [folder|session-id]", Short: "Show all environments or details and pending changes for one", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && *localName != "" {
			return fmt.Errorf("--name requires a folder target")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			report, err := e.StatusAll(cmd.Context(), "")
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
			if err := printDefaultErrors(cmd.OutOrStdout(), report.DefaultErrors); err != nil {
				return err
			}
			return printUnmatchedContainers(cmd.OutOrStdout(), report.UnmatchedContainers)
		}
		details, err := e.Status(cmd.Context(), args[0], *localName)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(details)
		}
		return printStatusDetails(cmd.OutOrStdout(), details, scopedSteps(cmd, []commanderror.Step{commanderror.Next("To apply changes", "recreate", details.Target)}, e.Store.Home))
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print session details and status, or bulk inventory diagnostics, as JSON")
	return sessionNameFlag(cmd, localName)
}

func printStatusDetails(out io.Writer, details app.StatusDetails, steps []commanderror.Step) error {
	view := details.View
	if _, err := fmt.Fprintf(out, "%s  %s  %s\n", displayCell(view.Target), containerState(view), displayCell(view.Workspace)); err != nil {
		return err
	}
	if view.Pending != nil {
		p := view.Pending
		fmt.Fprintf(out, "  Pending %s (%s): %s -> %s\n  Retry the same transfer command.\n", displayCell(store.TransferCommand(p.Mode)), displayCell(p.Phase), displayCell(p.Source), displayCell(p.Destination))
	}
	if view.Error != "" {
		fmt.Fprintf(out, "  Error: %s\n", displayCell(view.Error))
	}
	if details.DefaultError != "" {
		fmt.Fprintf(out, "Default selection unavailable: %s\n", displayCell(details.DefaultError))
	}
	if details.Record != nil {
		fmt.Fprintf(out, "Session: %s\nHarness: %s\nImage: %s\nActive commands: %d\n", displayCell(details.SessionID), displayCell(details.Harness), displayCell(details.Record.Applied.ImageID), len(details.Active))
		lifetime := "automatic (stops after the last attached command)"
		if details.Record.Settings.ManualStart {
			lifetime = "until stop (restarts with Docker)"
		}
		fmt.Fprintf(out, "Lifetime: %s\n", lifetime)
	}
	fmt.Fprintf(out, "Changes: %s\n", statusChange(view))
	if view.ConfigError != "" {
		fmt.Fprintf(out, "Desired configuration error: %s\n", displayCell(view.ConfigError))
	}
	for _, change := range view.PendingInputChanges {
		fmt.Fprintf(out, "  - [%s] %s\n", change.Scope, change)
	}
	if view.Error == "" && view.ConfigError == "" && view.Pending == nil {
		switch view.Desired {
		case environment.RuntimeSync:
			for _, change := range view.PendingInputChanges {
				if change.Field == "managed_config" {
					fmt.Fprintln(out, "Changes apply on container restart.")
					break
				}
			}
		case environment.Recreate, environment.RebuildAndRecreate:
			_, err := fmt.Fprint(out, stepsText(steps))
			return err
		}
	}
	return nil
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
		fmt.Fprintf(w, "%s\t%s\t%s\n", displayCell(view.Target), containerState(view), statusChange(view))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := printListRows(out, views, table.String(), false); err != nil {
		return err
	}
	for _, view := range views {
		if len(view.PendingInputChanges) > 0 {
			if _, err := fmt.Fprintf(out, "%s:\n", displayCell(view.Target)); err != nil {
				return err
			}
			for _, inputChange := range view.PendingInputChanges {
				if _, err := fmt.Fprintf(out, "  - [%s] %s\n", inputChange.Scope, inputChange); err != nil {
					return err
				}
			}
		}
		if view.ConfigError != "" {
			if _, err := fmt.Fprintf(out, "! %s: desired configuration: %s\n", displayCell(view.Target), displayCell(view.ConfigError)); err != nil {
				return err
			}
		}
		if view.Error == "" && view.ConfigError == "" && view.Pending == nil && (view.Desired == environment.Recreate || view.Desired == environment.RebuildAndRecreate) {
			if _, err := fmt.Fprintf(out, "  dbx recreate %s\n", displayCell(view.Target)); err != nil {
				return err
			}
		}
	}
	return nil
}
