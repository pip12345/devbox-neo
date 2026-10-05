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
	cmd := &cobra.Command{Use: "status [folder|session]", Short: "Show all environments or details and pending changes for one", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && *localName != "" {
			return fmt.Errorf("--name requires a folder target")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			report, err := e.StatusAll(cmd.Context(), "")
			for _, view := range report.Sessions {
				writeWarnings(cmd.ErrOrStderr(), view.Warnings)
			}
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			if len(report.Sessions) == 0 {
				cmd.Println("No matching saved environments.")
			} else if err := printStatusList(cmd, cmd.OutOrStdout(), report.Sessions, e.Store.Home); err != nil {
				return err
			}
			if err := printDefaultErrors(cmd.OutOrStdout(), report.DefaultErrors); err != nil {
				return err
			}
			return printUnmatchedContainers(cmd.OutOrStdout(), report.UnmatchedContainers)
		}
		details, err := e.Status(cmd.Context(), args[0], *localName)
		writeWarnings(cmd.ErrOrStderr(), details.Warnings)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(details)
		}
		return printStatusDetails(cmd.OutOrStdout(), details, statusRecreateSteps(cmd, details.View, e.Store.Home))
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print session details and status, or bulk inventory diagnostics, as JSON")
	return sessionNameFlag(cmd, localName)
}

func statusRecreateSteps(cmd *cobra.Command, view app.View, home string) []commanderror.Step {
	reason := "To apply changes"
	if !view.Exists {
		reason = "To rebuild missing runtime"
	}
	return scopedSteps(cmd, []commanderror.Step{commanderror.Next(reason, "recreate", view.Target)}, home)
}

func printStatusDetails(out io.Writer, details app.StatusDetails, steps []commanderror.Step) error {
	view := details.View
	if _, err := fmt.Fprintf(out, "%s  %s\n", sessionLabel(view), containerState(view)); err != nil {
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
		fmt.Fprintf(out, "Session: %s\nHarness: %s\nContainer: %s (%s)\nImage: %s\nActive commands: %d\n", displayCell(details.Record.Directory), displayCell(details.Harness), displayCell(view.ContainerName), displayCell(view.ContainerID), displayCell(details.Record.Applied.ImageID), len(details.Active))
		for _, lease := range details.Active {
			fmt.Fprintf(out, "  %s — host PID %d, since %s\n", displayCell(lease.Action), lease.Process.PID, exactTime(lease.Created))
		}
		lifetime := "automatic (stops after the last attached command)"
		if details.Record.Settings.ManualStart {
			lifetime = "until stop (restarts with Docker)"
		}
		fmt.Fprintf(out, "Lifetime: %s\n", lifetime)
	}
	if view.ImageMissing {
		fmt.Fprintln(out, "Recorded image missing; existing container access is unaffected. Recreate builds an image only if container replacement needs one.")
	}
	if !view.Exists && view.Error == "" && view.Pending == nil {
		fmt.Fprintln(out, "Container missing; use explicit Recreate to rebuild from current config before accessing it.")
	}
	fmt.Fprintf(out, "Changes: %s\n", statusChange(view))
	if view.ConfigError != "" {
		fmt.Fprintf(out, "Desired configuration error: %s\n", displayCell(view.ConfigError))
	}
	for _, change := range view.PendingInputChanges {
		fmt.Fprintf(out, "  - [%s] %s\n", change.Scope, change)
	}
	if view.Error == "" && view.Pending == nil && !view.Exists {
		_, err := fmt.Fprint(out, stepsText(steps))
		return err
	}
	if view.Error == "" && view.ConfigError == "" && view.Pending == nil {
		switch view.Desired {
		case environment.RuntimeSync, environment.Recreate, environment.RebuildAndRecreate:
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

func printStatusList(cmd *cobra.Command, out io.Writer, views []app.View, home string) error {
	var table bytes.Buffer
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tFOLDER\tNAME\tHARNESS\tCONTAINER\tCHANGE")
	for _, view := range views {
		name := view.LocalName
		if name == "" {
			name = view.Target
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", displayCell(view.Target), displayCell(view.Workspace), displayCell(name), displayCell(view.Harness), containerState(view), statusChange(view))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := printListRows(out, views, table.String(), false); err != nil {
		return err
	}
	for _, view := range views {
		if view.ImageMissing {
			fmt.Fprintf(out, "%s: recorded image missing; existing container access is unaffected. Recreate builds an image only if container replacement needs one.\n", sessionLabel(view))
		}
		if !view.Exists && view.Error == "" && view.Pending == nil {
			fmt.Fprintf(out, "%s: container missing; use explicit Recreate to rebuild from current config before accessing it.\n", sessionLabel(view))
		}
		if len(view.PendingInputChanges) > 0 {
			if _, err := fmt.Fprintf(out, "%s:\n", sessionLabel(view)); err != nil {
				return err
			}
			for _, inputChange := range view.PendingInputChanges {
				if _, err := fmt.Fprintf(out, "  - [%s] %s\n", inputChange.Scope, inputChange); err != nil {
					return err
				}
			}
		}
		if view.ConfigError != "" {
			if _, err := fmt.Fprintf(out, "! %s: desired configuration: %s\n", sessionLabel(view), displayCell(view.ConfigError)); err != nil {
				return err
			}
		}
		if view.Error == "" && view.Pending == nil && (!view.Exists || (view.ConfigError == "" && (view.Desired == environment.RuntimeSync || view.Desired == environment.Recreate || view.Desired == environment.RebuildAndRecreate))) {
			if _, err := fmt.Fprint(out, stepsText(statusRecreateSteps(cmd, view, home))); err != nil {
				return err
			}
		}
	}
	return nil
}
