package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func sessionCommands(factory engineFactory, profile *string) *cobra.Command {
	group := &cobra.Command{Use: "session", Short: "Show, copy, move, or delete saved session data"}
	var listJSON bool
	var sortBy string
	list := &cobra.Command{Use: "list", Short: "List sessions with their harness, last activity, and container status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if sortBy != "name" && sortBy != "last-active" {
			return fmt.Errorf("unknown session sort %q: use name or last-active", sortBy)
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		views, err := e.List(cmd.Context(), true)
		if err != nil {
			return err
		}
		if *profile != "" {
			filtered := []app.View{}
			for _, view := range views {
				if view.Profile == *profile {
					filtered = append(filtered, view)
				}
			}
			views = filtered
		}
		sortViews(views, sortBy)
		if listJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(views)
		}
		if len(views) == 0 {
			cmd.Println("No durable sessions. Configure a profile/project, then open its folder.")
			return nil
		}
		return printSessionList(cmd.OutOrStdout(), views, time.Now())
	}}
	list.Flags().BoolVar(&listJSON, "json", false, "Print session entries as JSON")
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
	var options app.ResetOptions
	var resetJSON bool
	reset := &cobra.Command{Use: "reset [target...]", Short: "Reset harness state, keeping saved history (containers must be stopped)", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		options.Targets = args
		options.Profile = *profile
		results, err := e.ResetSessions(cmd.Context(), options)
		if err != nil {
			return err
		}
		if resetJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(results)
		}
		for _, result := range results {
			action := "Reset"
			if result.DryRun {
				action = "Would reset"
			}
			cmd.Printf("%s %s (%d paths; auth and shared caches untouched)\n", action, result.Name, len(result.Removed))
		}
		return nil
	}}
	reset.Flags().BoolVar(&options.All, "all", false, "Reset all sessions, add --profile NAME to reset all belonging to one profile")
	reset.Flags().StringVar(&options.Harness, "harness", "", "Reset this harness instead of the session's current harness")
	reset.Flags().BoolVar(&options.AllHarnesses, "all-harnesses", false, "Reset every harness with saved state in these sessions")
	reset.Flags().BoolVar(&options.IncludeHistory, "include-history", false, "Also delete saved history")
	reset.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview without changing state")
	reset.Flags().BoolVar(&resetJSON, "json", false, "Print reset results as JSON")
	var deleteDryRun, deleteJSON bool
	remove := &cobra.Command{Use: "delete <target...>", Short: "Delete session data after its containers have been removed", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		names, err := e.DeleteSessions(cmd.Context(), args, *profile, deleteDryRun)
		if err != nil {
			return err
		}
		return printSessionDeletion(cmd, names, deleteDryRun, deleteJSON)
	}}
	remove.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Preview without deleting session data")
	remove.Flags().BoolVar(&deleteJSON, "json", false, "Print session names as JSON")
	var pruneOptions app.PruneOptions
	var pruneJSON bool
	prune := &cobra.Command{Use: "prune", Short: "Delete sessions matching --orphaned and/or --older-than", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		pruneOptions.Profile = *profile
		names, err := e.PruneSessions(cmd.Context(), pruneOptions)
		if err != nil {
			return err
		}
		return printSessionDeletion(cmd, names, pruneOptions.DryRun, pruneJSON)
	}}
	prune.Flags().BoolVar(&pruneOptions.Orphaned, "orphaned", false, "Only sessions without a Devbox container")
	prune.Flags().DurationVar(&pruneOptions.OlderThan, "older-than", time.Duration(0), "Only sessions inactive longer than this duration, e.g. 24h")
	prune.Flags().BoolVar(&pruneOptions.DryRun, "dry-run", false, "Preview without deleting state")
	prune.Flags().BoolVar(&pruneOptions.Confirm, "yes", false, "Confirm deletion of the matching sessions")
	prune.Flags().BoolVar(&pruneJSON, "json", false, "Print session names as JSON")
	group.AddCommand(list, show, reset, remove, prune)
	for _, mode := range []string{"clone", "relocate"} {
		group.AddCommand(transferCommand(factory, profile, mode))
	}
	return group
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
func printSessionDeletion(cmd *cobra.Command, names []string, dryRun, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(names)
	}
	action := "Deleted session"
	if dryRun {
		action = "Would delete session"
	}
	for _, name := range names {
		cmd.Printf("%s %s\n", action, name)
	}
	if len(names) == 0 {
		cmd.Println("No matching sessions.")
	}
	return nil
}
