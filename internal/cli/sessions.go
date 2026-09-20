package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"devbox/internal/app"
	"devbox/internal/store"
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
	return []*cobra.Command{list, statusCommand(factory, profile), deleteCommand(factory, profile), transferCommand(factory, profile)}
}
func transferCommand(factory engineFactory, profile *string) *cobra.Command {
	var options app.TransferOptions
	var asJSON, move bool
	description := "Copy session state to another folder or profile"
	cmd := &cobra.Command{Use: "copy <folder|session> [destination-folder]", Short: description, Long: description + ".\nBy default, keep the source and leave the destination stopped.\nWith --move, remove the source after the destination is ready and preserve its running intent.\nRetry the same command to resume an interrupted transfer.", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		options.Mode = "clone"
		if move {
			options.Mode = "relocate"
		}
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
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s -> %s\n", action, store.TransferCommand(result.Mode), result.Source, result.Destination)
		return err
	}}
	cmd.Flags().BoolVar(&move, "move", false, "Remove the source after the destination is ready")
	cmd.Flags().StringVar(&options.From, "from", "", "Source slot: .profile-NAME, .profile-NAME.project, or .project")
	cmd.Flags().StringVar(&options.To, "to", "", "Destination slot: .profile-NAME, .profile-NAME.project, or .project")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview without copying session data")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print transfer result as JSON")
	return cmd
}
