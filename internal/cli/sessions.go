package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func sessionCommands(factory engineFactory, profile *string) []*cobra.Command {
	var listJSON, wide bool
	var sortBy string
	list := &cobra.Command{Use: "list", Short: "List sessions with their harness, last activity, and container status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if sortBy != "name" && sortBy != "last-active" {
			return fmt.Errorf("unknown session sort %q: use name or last-active", sortBy)
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		views, err := e.List(cmd.Context(), *profile)
		if err != nil {
			return err
		}
		sortViews(views.Sessions, sortBy)
		if listJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(views)
		}
		if len(views.Sessions) == 0 {
			cmd.Println("No durable sessions. Configure a profile/project, then use create <folder>.")
		} else if err := printSessionList(cmd.OutOrStdout(), views.Sessions, wide, time.Now()); err != nil {
			return err
		}
		return printUnmatchedContainers(cmd.OutOrStdout(), views.UnmatchedContainers)
	}}
	list.Flags().BoolVar(&listJSON, "json", false, "Print saved environments and unmatched containers as JSON")
	list.Flags().BoolVar(&wide, "wide", false, "Also show exact activity/creation timestamps and the last action")
	list.Flags().StringVar(&sortBy, "sort", "name", "Sort by name or last-active (newest first)")
	var showJSON bool
	show := &cobra.Command{Use: "show <target>", Short: "Show saved session settings and active commands", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		details, err := e.SessionShow(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		if showJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(details)
		}
		printView(cmd, details.Container)
		cmd.Printf("Session: %s\nHarness: %s\nImage: %s\nActive commands: %d\n", details.Record.ID, details.Record.Definition.Name, details.Record.ImageID, len(details.Active))
		return nil
	}}
	show.Flags().BoolVar(&showJSON, "json", false, "Print session settings and container status as JSON")
	commands := []*cobra.Command{list, statusCommand(factory, profile), show, deleteCommand(factory, profile)}
	for _, mode := range []string{"clone", "relocate"} {
		commands = append(commands, transferCommand(factory, profile, mode))
	}
	return commands
}
func transferCommand(factory engineFactory, profile *string, mode string) *cobra.Command {
	var options app.TransferOptions
	var asJSON bool
	description := "Copy session state to another folder or profile"
	if mode == "relocate" {
		description = "Move session state to another folder or profile"
	}
	cmd := &cobra.Command{Use: mode + " <source> [destination-folder]", Short: description, Long: description + ".\nRetry the same command to resume an interrupted transfer.", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		options.Mode = mode
		options.Source = args[0]
		options.Profile = *profile
		options.Destination = ""
		if len(args) == 2 {
			options.Destination = args[1]
		}
		result, err := e.Transfer(cmd.Context(), options)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		action := "Completed"
		if result.DryRun {
			action = "Would perform"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s -> %s\n", action, mode, result.Source, result.Destination)
		return err
	}}
	cmd.Flags().StringVar(&options.From, "from", "", "Source profile name or .project")
	cmd.Flags().StringVar(&options.To, "to", "", "Destination profile name or .project in the same folder")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview without copying session data")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print transfer result as JSON")
	return cmd
}
