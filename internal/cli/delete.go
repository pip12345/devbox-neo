package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

type deletionConfirmation struct {
	ui                *cliui.Runner
	defaultFolder     string
	singleTarget      bool
	removedContainers int
}

func (c *deletionConfirmation) confirm(prompt app.DeletePrompt) (bool, error) {
	var text strings.Builder
	if c.defaultFolder != "" {
		if len(prompt.Containers) > 0 || c.removedContainers == 0 {
			name := prompt.Containers
			if len(name) == 0 {
				name = prompt.Sessions
			}
			if len(name) == 1 {
				local := name[0]
				if dot := strings.LastIndexByte(local, '.'); dot >= 0 {
					local = local[dot+1:]
				}
				fmt.Fprintf(&text, "Delete %s (default in %s)\n", displayCell(local), displayCell(c.defaultFolder))
			}
		}
	}
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
	} else if len(prompt.Sessions) > 0 {
		if c.removedContainers > 0 {
			if c.removedContainers == 1 {
				text.WriteString("Container removed. ")
			} else {
				fmt.Fprintf(&text, "%d containers removed.\n", c.removedContainers)
			}
		} else if c.singleTarget {
			text.WriteString("No container. ")
		}
		if !c.singleTarget {
			fmt.Fprintln(&text, "Saved sessions:")
			for _, name := range prompt.Sessions {
				fmt.Fprintf(&text, "  %s\n", displayCell(name))
			}
		} else if c.defaultFolder == "" && c.removedContainers == 0 {
			fmt.Fprintf(&text, "Saved session: %s\n", displayCell(prompt.Sessions[0]))
		}
		text.WriteString("Delete saved data and history? [y/N] ")
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
	for _, name := range result.Retained {
		fmt.Fprintf(&text, "Session state and image retained: %s\n", displayCell(name))
	}
	if !result.Cancelled && len(result.Containers) == 0 && len(result.Sessions) == 0 {
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
		if options.Scope == "" {
			confirmation := &deletionConfirmation{ui: cliui.New(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout()), singleTarget: len(args) == 1}
			defer func() { runErr = errors.Join(runErr, confirmation.ui.Finish()) }()
			if confirmation.singleTarget && *localName == "" && !environment.IsSessionTarget(args[0]) {
				confirmation.defaultFolder = args[0]
			}
			options.Confirm = confirmation.confirm
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		result, err := e.Delete(cmd.Context(), options)
		if err != nil {
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
