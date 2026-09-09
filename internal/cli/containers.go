package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

type engineFactory func(*cobra.Command) (*app.Engine, error)

func containerCommands(factory engineFactory, profile *string) []*cobra.Command {
	var listJSON, wide bool
	var sortBy string
	list := &cobra.Command{Use: "list", Short: "List containers with profile and last activity", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if sortBy != "name" && sortBy != "last-active" {
			return fmt.Errorf("unknown container sort %q: use name or last-active", sortBy)
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		views, err := e.List(cmd.Context(), false)
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
			cmd.Println("No managed containers. Configure a profile/project, then open its folder.")
			return nil
		}
		return printContainerList(cmd.OutOrStdout(), views, wide, time.Now())
	}}
	list.Flags().BoolVar(&listJSON, "json", false, "Print container entries as JSON")
	list.Flags().StringVar(&sortBy, "sort", "name", "Sort by name or last-active (newest first)")
	list.Flags().BoolVar(&wide, "wide", false, "Include harness, exact activity/creation times, and last action")
	var statusJSON, statusAll bool
	status := &cobra.Command{Use: "status [target]", Short: "Show live state and desired drift for one target or --all", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if statusAll && len(args) != 0 {
			return fmt.Errorf("--all does not accept an exact target")
		}
		if !statusAll && len(args) != 1 {
			return fmt.Errorf("provide a target or --all")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if statusAll {
			views, err := e.StatusAll(cmd.Context(), *profile)
			if err != nil {
				return err
			}
			if statusJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(views)
			}
			if len(views) == 0 {
				cmd.Println("No matching managed containers.")
				return nil
			}
			return printStatusList(cmd.OutOrStdout(), views)
		}
		view, err := e.Status(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		if statusJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
		}
		printView(cmd, view)
		if view.ConfigError != "" {
			cmd.Printf("Desired configuration error: %s\n", view.ConfigError)
		} else {
			cmd.Printf("Desired change: %s\n", view.Desired)
			for _, inputChange := range view.PendingInputChanges {
				cmd.Printf("  - [%s] %s\n", inputChange.Scope, inputChange)
			}
		}
		return nil
	}}
	status.Flags().BoolVar(&statusJSON, "json", false, "Print status as JSON")
	status.Flags().BoolVar(&statusAll, "all", false, "Check all managed containers, optionally filtered by --profile")
	var follow bool
	var tail string
	logs := &cobra.Command{Use: "logs <target>", Short: "Read the container's Docker logs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if tail != "all" {
			n, err := strconv.Atoi(tail)
			if err != nil || n < 0 {
				return fmt.Errorf("--tail must be all or a non-negative count")
			}
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		return e.Logs(cmd.Context(), args[0], *profile, follow, tail)
	}}
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "Follow logs until interrupted")
	logs.Flags().StringVar(&tail, "tail", "100", "Number of trailing lines, or all")
	var all, stopped, force, deleteJSON bool
	remove := &cobra.Command{Use: "delete [target...]", Short: "Delete containers while preserving sessions and image tags", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		removed, err := e.DeleteContainers(cmd.Context(), app.Selection{Targets: args, Profile: *profile, All: all, Stopped: stopped}, force)
		if err != nil {
			return err
		}
		if deleteJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(removed)
		}
		for _, name := range removed {
			cmd.Printf("Deleted %s; session state and image retained.\n", name)
		}
		if len(removed) == 0 {
			cmd.Println("No matching containers.")
		}
		return nil
	}}
	remove.Flags().BoolVar(&all, "all", false, "Select all owned containers")
	remove.Flags().BoolVar(&stopped, "stopped", false, "Select stopped owned containers")
	remove.Flags().BoolVar(&force, "force", false, "Permit disruption of active attached commands")
	remove.Flags().BoolVar(&deleteJSON, "json", false, "Print deleted names as JSON")
	return []*cobra.Command{list, status, logs, remove, networkCommands(factory, profile)}
}
func printView(cmd *cobra.Command, view app.View) {
	state := "missing"
	if view.Exists {
		state = "stopped"
		if view.Running {
			state = "running"
		}
	}
	cmd.Printf("%s  %s  %s\n", view.Name, state, view.Workspace)
	if view.Pending != nil {
		cmd.Printf("  Pending %s (%s): %s -> %s\n  Retry the same transfer command.\n", view.Pending.Mode, view.Pending.Phase, view.Pending.Source, view.Pending.Destination)
	}
	if view.Error != "" {
		cmd.Printf("  Error: %s\n", view.Error)
	}
}
func networkCommands(factory engineFactory, profile *string) *cobra.Command {
	group := &cobra.Command{Use: "network", Short: "Inspect networking or attach existing secondary networks"}
	inspect := &cobra.Command{Use: "inspect <target>", Short: "Print live network facts as JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		facts, err := e.NetworkFacts(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(facts)
	}}
	var key string
	env := &cobra.Command{Use: "env <target>", Short: "Print shell-safe network exports", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		facts, err := e.NetworkFacts(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		values := facts.Env()
		if key != "" {
			value, ok := values[key]
			if !ok {
				return fmt.Errorf("unknown network variable")
			}
			cmd.Println(value)
			return nil
		}
		keys := make([]string, 0, len(values))
		for name := range values {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			cmd.Printf("export %s=%s\n", name, shellQuote(values[name]))
		}
		return nil
	}}
	env.Flags().StringVar(&key, "get", "", "Print one variable's value without shell syntax")
	group.AddCommand(inspect, env)
	for _, action := range []string{"connect", "disconnect"} {
		action := action
		group.AddCommand(&cobra.Command{Use: action + " <network> <target>", Short: action + " an existing secondary network without editing configuration", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			e, err := factory(cmd)
			if err != nil {
				return err
			}
			return e.ChangeNetwork(cmd.Context(), args[1], *profile, args[0], action == "connect")
		}})
	}
	return group
}
