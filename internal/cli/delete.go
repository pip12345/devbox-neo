package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

type deletionConfirmation struct {
	ui                *cliui.Runner
	singleTarget      bool
	removedContainers int
}

func (c *deletionConfirmation) confirm(prompt app.DeletePrompt) (bool, error) {
	var text strings.Builder
	if len(prompt.Containers) > 0 {
		if len(prompt.Containers) == 1 {
			fmt.Fprintf(&text, "Container: %s\n", displayCell(prompt.Containers[0]))
			text.WriteString("Remove container? [y/N] ")
		} else {
			fmt.Fprintln(&text, "Containers:")
			for _, name := range prompt.Containers {
				fmt.Fprintf(&text, "  %s\n", displayCell(name))
			}
			text.WriteString("Remove containers? [y/N] ")
		}
	} else if len(prompt.Sessions)+len(prompt.IncompleteDirectories) > 0 {
		if c.removedContainers > 0 {
			if c.removedContainers == 1 {
				text.WriteString("Container removed. ")
			} else {
				fmt.Fprintf(&text, "%d containers removed.\n", c.removedContainers)
			}
		} else if c.singleTarget && len(prompt.Sessions) > 0 {
			text.WriteString("No container. ")
		}
		if len(prompt.Sessions) > 0 && !c.singleTarget {
			fmt.Fprintln(&text, "Saved sessions:")
			for _, name := range prompt.Sessions {
				fmt.Fprintf(&text, "  %s\n", displayCell(name))
			}
		} else if len(prompt.Sessions) > 0 && c.removedContainers == 0 {
			fmt.Fprintf(&text, "Saved session: %s\n", displayCell(prompt.Sessions[0]))
		}
		for _, name := range prompt.IncompleteDirectories {
			fmt.Fprintf(&text, "Incomplete creation directory: %s\n", displayCell(name))
		}
		if len(prompt.IncompleteDirectories) > 0 {
			text.WriteString("This removes the incomplete directories and all their copied files. Images from incomplete creations will be retained.\n")
		}
		if len(prompt.Sessions) == 0 {
			text.WriteString("Delete incomplete creation files? [y/N] ")
		} else {
			text.WriteString("Delete saved data and history? [y/N] ")
		}
	}
	confirmed, err := c.ui.Confirm(text.String())
	if err != nil {
		return false, err
	}
	if confirmed && len(prompt.Containers) > 0 {
		// app.Delete asks about saved state only after this phase succeeds.
		c.removedContainers = len(prompt.Containers)
	}
	return confirmed, nil
}

func printDeleteResult(out io.Writer, result app.DeleteResult) error {
	var text strings.Builder
	if result.Cancelled {
		text.WriteString("Cancelled.\n")
	}
	action := "Deleted"
	if result.DryRun {
		action = "Would delete"
	}
	for _, name := range result.Containers {
		fmt.Fprintf(&text, "%s container %s.\n", action, displayCell(name))
	}
	for _, name := range result.Sessions {
		fmt.Fprintf(&text, "%s session %s.\n", action, displayCell(name))
	}
	for _, name := range result.IncompleteDirectories {
		fmt.Fprintf(&text, "%s incomplete creation directory %s; images retained.\n", action, displayCell(name))
	}
	for _, name := range result.RetainedIncompleteDirectories {
		fmt.Fprintf(&text, "Incomplete creation directory retained: %s. Choose container and saved data/history (--session) to remove its files; images will be retained.\n", displayCell(name))
	}
	for _, name := range result.Retained {
		fmt.Fprintf(&text, "Session state and image retained: %s\n", displayCell(name))
	}
	if !result.Cancelled && result.DeletedCount() == 0 {
		text.WriteString("No resources deleted.\n")
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func deleteCommand(factory engineFactory, localName *string) *cobra.Command {
	var options app.DeleteOptions
	var container, session, asJSON bool
	cmd := &cobra.Command{Use: "delete [folder|session...]", Short: "Delete containers, optionally also deleting saved session data", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		if container && session {
			return fmt.Errorf("choose --container or --session, not both")
		}
		if cmd.Flags().Changed("older-than") && options.OlderThan <= 0 {
			return fmt.Errorf("--older-than must be positive")
		}
		options.Scope = ""
		if container {
			options.Scope = app.DeleteContainer
		} else if session {
			options.Scope = app.DeleteSession
		}
		if options.Scope == "" && (options.DryRun || !interactive(cmd) || asJSON) {
			return commanderror.New("deletion_scope_required", "Choose --container or --session for non-interactive deletion or a dry run.", "", nil)
		}
		options.Selection.Targets = args
		options.Selection.LocalName = *localName
		options.Confirm = nil
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if options.Scope == "" {
			options.Scope = app.DeleteContainer
			preview := options
			preview.DryRun = true
			result, err := e.Delete(cmd.Context(), preview)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				// Pin folder/default lookups to the targets shown by preflight;
				// changing a default while the form is open must not retarget deletion.
				targets := slices.Clone(result.Targets)
				if len(targets) == 0 {
					return printDeleteResult(cmd.OutOrStdout(), result)
				}
				options.Selection = app.Selection{Captured: targets}
			}
			m := newMenu(cmd)
			defer func() { runErr = errors.Join(runErr, m.Finish()) }()
			f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
			return f.deleteWithOptions(options)
		}
		result, err := e.Delete(cmd.Context(), options)
		if err != nil {
			if !result.DryRun && result.DeletedCount() > 0 {
				if !asJSON {
					err = errors.Join(err, printDeleteResult(cmd.OutOrStdout(), result))
				}
				return &partialResultError{cause: err, result: result}
			}
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return printDeleteResult(cmd.OutOrStdout(), result)
	}}
	cmd.Flags().BoolVar(&options.Selection.All, "all", false, "Select all saved environments and unmatched managed containers")
	cmd.Flags().BoolVar(&options.Selection.Stopped, "stopped", false, "Select environments with stopped containers")
	cmd.Flags().BoolVar(&container, "container", false, "Delete containers without prompting; retain saved session data")
	cmd.Flags().BoolVar(&session, "session", false, "Delete the whole environment without prompting: container and saved session data/history")
	cmd.Flags().BoolVar(&options.Orphaned, "orphaned", false, "Select saved environments without containers")
	cmd.Flags().DurationVar(&options.OlderThan, "older-than", 0, "Select environments inactive longer than this duration, e.g. 720h; rechecked while locked")
	cmd.Flags().BoolVar(&options.Force, "force", false, "Allow container deletion despite attached commands; never implies deleting saved data")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview the explicitly selected deletion without changing resources")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print deletion results as JSON; never prompt")
	return sessionNameFlag(cmd, localName)
}
