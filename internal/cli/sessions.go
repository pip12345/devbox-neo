package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func sessionCommands(factory engineFactory, profile *string) *cobra.Command {
	group := &cobra.Command{Use: "session", Short: "Inspect, transfer, or clean durable session state"}
	var listJSON bool
	var sortBy string
	list := &cobra.Command{Use: "list", Short: "List durable sessions with harness, activity, and container state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
	show := &cobra.Command{Use: "show <target>", Short: "Inspect the durable contract and active leases without desired configuration", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	show.Flags().BoolVar(&showJSON, "json", false, "Print durable contract and live state as JSON")
	var options app.ResetOptions
	var resetJSON bool
	reset := &cobra.Command{Use: "reset [target...]", Short: "Reset stopped session stores while preserving declared history", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
	reset.Flags().BoolVar(&options.All, "all", false, "Select all durable sessions")
	reset.Flags().StringVar(&options.Harness, "harness", "", "Reset a selected harness instead of the recorded harness")
	reset.Flags().BoolVar(&options.AllHarnesses, "all-harnesses", false, "Reset every harness stored in the selected sessions")
	reset.Flags().BoolVar(&options.IncludeHistory, "include-history", false, "Also clear the selected environment stores' history")
	reset.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview without changing state")
	reset.Flags().BoolVar(&resetJSON, "json", false, "Print reset results as JSON")
	var deleteDryRun, deleteJSON bool
	remove := &cobra.Command{Use: "delete <target...>", Short: "Delete exact sessions after their containers are gone", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	remove.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Preview exact deletion")
	remove.Flags().BoolVar(&deleteJSON, "json", false, "Print selected session names as JSON")
	var pruneOptions app.PruneOptions
	var pruneJSON bool
	prune := &cobra.Command{Use: "prune", Short: "Preview or confirm filtered durable-state cleanup", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
	prune.Flags().BoolVar(&pruneOptions.Orphaned, "orphaned", false, "Select sessions without owned containers")
	prune.Flags().DurationVar(&pruneOptions.OlderThan, "older-than", time.Duration(0), "Select sessions inactive longer than this duration")
	prune.Flags().BoolVar(&pruneOptions.DryRun, "dry-run", false, "Preview without deleting state")
	prune.Flags().BoolVar(&pruneOptions.Confirm, "yes", false, "Confirm filtered state deletion")
	prune.Flags().BoolVar(&pruneJSON, "json", false, "Print selected session names as JSON")
	group.AddCommand(list, show, reset, remove, prune)
	for _, mode := range []string{"clone", "relocate"} {
		group.AddCommand(transferCommand(factory, profile, mode))
	}
	return group
}
func transferCommand(factory engineFactory, profile *string, mode string) *cobra.Command {
	var options app.TransferOptions
	var asJSON bool
	cmd := &cobra.Command{Use: mode + " <source> [destination-folder]", Short: mode + " portable session state", Long: mode + " portable session state.\nRetry the same command to resume pending work.", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
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
	cmd.Flags().StringVar(&options.From, "from", "", "Exact source slot (profile name or .project)")
	cmd.Flags().StringVar(&options.To, "to", "", "Exact destination slot in the same folder")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and preview without copying state")
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
