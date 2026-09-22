package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"github.com/spf13/cobra"
)

func confirmDeletion(reader *bufio.Reader, out io.Writer, prompt app.DeletePrompt) (bool, error) {
	var text strings.Builder
	if len(prompt.Containers) > 0 {
		fmt.Fprintln(&text, "Delete containers:")
		for _, name := range prompt.Containers {
			fmt.Fprintf(&text, "  %s\n", displayCell(name))
		}
	}
	if len(prompt.Sessions) > 0 {
		fmt.Fprintln(&text, "Delete saved session data (including harness state and conversation history):")
		for _, name := range prompt.Sessions {
			fmt.Fprintf(&text, "  %s\n", displayCell(name))
		}
	}
	text.WriteString("Continue? [y/N] ")
	if _, err := io.WriteString(out, text.String()); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func deleteCommand(factory engineFactory, localName *string) *cobra.Command {
	var options app.DeleteOptions
	var container, session, asJSON bool
	cmd := &cobra.Command{Use: "delete [folder|session...]", Short: "Delete containers, optionally also deleting saved session data", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
			reader := promptReader(cmd)
			options.Confirm = func(prompt app.DeletePrompt) (bool, error) {
				return confirmDeletion(reader, cmd.OutOrStdout(), prompt)
			}
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
		if result.Cancelled {
			cmd.Println("Cancelled.")
			return nil
		}
		action := "Deleted"
		if result.DryRun {
			action = "Would delete"
		}
		for _, name := range result.Containers {
			cmd.Printf("%s container %s.\n", action, displayCell(name))
		}
		for _, name := range result.Sessions {
			cmd.Printf("%s session %s.\n", action, displayCell(name))
		}
		for _, name := range result.Retained {
			cmd.Printf("Session state and image retained: %s\n", displayCell(name))
		}
		if len(result.Containers) == 0 && len(result.Sessions) == 0 {
			cmd.Println("No resources deleted.")
		}
		return nil
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
