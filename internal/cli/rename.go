package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

const renameEffects = "Rebuilds the container with current config; container-local changes are lost. Saved harness state and session ID are preserved. If this is the folder default, that default is cleared."

func renameCommand(factory engineFactory, name *string) *cobra.Command {
	var to string
	var dryRun, asJSON bool
	cmd := &cobra.Command{
		Use: "rename <folder|session> --to NAME", Short: "Rename a session by moving it to a new name in the same folder",
		Long: "Rename a session using the existing same-folder move operation.\n" + renameEffects + "\nFinish attached commands first. Retry the same command to resume an interrupted rename.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := environment.ValidateLocalName(to); err != nil {
				return err
			}
			e, err := factory(cmd)
			if err != nil {
				return err
			}
			if !dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), renameEffects)
			}
			result, err := e.Rename(cmd.Context(), args[0], *name, to, dryRun)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return printRenameResult(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "New folder-local session name (required)")
	_ = cmd.MarkFlagRequired("to")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without renaming or rebuilding")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the transfer result as JSON")
	return sessionNameFlag(cmd, name)
}

func printRenameResult(out io.Writer, result app.TransferResult) error {
	action := "Renamed"
	if result.DryRun {
		action = "Would rename"
	}
	_, err := fmt.Fprintf(out, "%s: %s -> %s\nSession: %s\nFolder: %s\n", action, result.Source, result.Destination, result.LocalName, displayCell(result.Workspace))
	if err == nil && result.DryRun {
		_, err = fmt.Fprintln(out, renameEffects)
	}
	return err
}

func (f *frontend) rename(target string) (renamed bool, err error) {
	pending, err := f.e.Store.Pending(target)
	if err != nil {
		return false, err
	}
	if pending != nil {
		return false, fmt.Errorf("session has a pending transfer; use Copy or move to finish it before renaming")
	}
	to := ""
	err = f.form("Rename session", func() []cliui.Action {
		submit := cliui.Action{Label: "Rename session", Description: renameEffects, Run: func() (bool, error) {
			preview, err := f.e.Rename(f.m.Context, target, "", to, true)
			if err != nil {
				return false, f.m.report(err)
			}
			yes, err := f.m.Confirm(fmt.Sprintf("Rename %s to %s?\n%s\nProceed? [y/N] ", displayCell(preview.Source), displayCell(preview.LocalName), renameEffects))
			if err != nil || !yes {
				return false, err
			}
			err = f.foreground("Rename session", func(ctx context.Context) error {
				result, err := f.e.Rename(ctx, target, "", to, false)
				if err != nil {
					return err
				}
				renamed = true
				f.focusItem = result.Destination
				return printRenameResult(f.cmd.OutOrStdout(), result)
			})
			if err != nil {
				return false, f.m.report(err)
			}
			return true, nil
		}}
		if to == "" {
			submit.Blocked = "Enter a new session name first."
		}
		return []cliui.Action{f.text("New session name", &to, environment.ValidateLocalName), submit}
	})
	return renamed, err
}
