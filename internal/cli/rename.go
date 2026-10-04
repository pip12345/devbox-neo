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

const renameEffects = "Changes only the session name; the container, saved history, and folder default are unchanged."

func renameCommand(factory engineFactory, name *string) *cobra.Command {
	var to string
	var dryRun, asJSON bool
	cmd := &cobra.Command{
		Use: "rename <folder|session> --to NAME", Short: "Change a session's folder-local name",
		Long: "Change a session's folder-local name.\n" + renameEffects,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := environment.ValidateLocalName(to); err != nil {
				return err
			}
			e, err := factory(cmd)
			if err != nil {
				return err
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
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without saving")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the rename result as JSON")
	return sessionNameFlag(cmd, name)
}

func printRenameResult(out io.Writer, result app.RenameResult) error {
	action := "Renamed"
	if result.DryRun {
		action = "Would rename"
	}
	_, err := fmt.Fprintf(out, "%s: %s -> %s\nSession: %s\nFolder: %s\n", action, result.PreviousName, result.LocalName, result.Session, displayCell(result.Workspace))
	return err
}

func (f *frontend) rename(target string) (renamed bool, err error) {
	to := ""
	err = f.form("Rename session", func() []cliui.Action {
		submit := f.submit("Rename session", renameEffects, func() error {
			return f.foreground("Rename session", func(ctx context.Context) error {
				result, err := f.e.Rename(ctx, target, "", to, false)
				if err != nil {
					return err
				}
				renamed = true
				f.focusItem = result.Session
				return printRenameResult(f.cmd.OutOrStdout(), result)
			})
		})
		if to == "" {
			submit.Blocked = "Enter a new session name first."
		}
		return []cliui.Action{f.text("New session name", &to, environment.ValidateLocalName), submit}
	})
	return renamed, err
}
