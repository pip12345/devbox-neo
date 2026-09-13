package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

type engineFactory func(*cobra.Command) (*app.Engine, error)

func containerCommands(factory engineFactory, profile *string) []*cobra.Command {
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
	return []*cobra.Command{logs, networkCommands(factory, profile)}
}

func printView(cmd *cobra.Command, view app.View) {
	cmd.Printf("%s  %s  %s\n", displayCell(view.Name), containerState(view), displayCell(view.Workspace))
	if view.Pending != nil {
		cmd.Printf("  Pending %s (%s): %s -> %s\n  Retry the same transfer command.\n", displayCell(view.Pending.Mode), displayCell(view.Pending.Phase), displayCell(view.Pending.Source), displayCell(view.Pending.Destination))
	}
	if view.Error != "" {
		cmd.Printf("  Error: %s\n", displayCell(view.Error))
	}
}

func networkCommands(factory engineFactory, profile *string) *cobra.Command {
	group := &cobra.Command{Use: "network", Short: "Show container networking or connect and disconnect additional networks"}
	inspect := &cobra.Command{Use: "inspect <target>", Short: "Show the container's networks, IP addresses, and gateways as JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	env := &cobra.Command{Use: "env <target>", Short: "Print network variables as shell export commands", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		group.AddCommand(&cobra.Command{Use: action + " <network> <target>", Short: action + " an existing Docker network without changing saved configuration", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			e, err := factory(cmd)
			if err != nil {
				return err
			}
			return e.ChangeNetwork(cmd.Context(), args[1], *profile, args[0], action == "connect")
		}})
	}
	return group
}
