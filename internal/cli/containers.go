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

func containerCommands(factory engineFactory, localName *string) []*cobra.Command {
	var follow bool
	var tail string
	logs := &cobra.Command{Use: "logs <folder|session>", Short: "Read the container's Docker logs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		return e.Logs(cmd.Context(), args[0], *localName, follow, tail)
	}}
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "Follow logs until interrupted")
	logs.Flags().StringVar(&tail, "tail", "100", "Number of trailing lines, or all")
	return []*cobra.Command{sessionNameFlag(logs, localName), networkCommands(factory, localName)}
}

func networkCommands(factory engineFactory, localName *string) *cobra.Command {
	group := &cobra.Command{Use: "network", Short: "Show container networking or connect and disconnect additional networks"}
	inspect := &cobra.Command{Use: "inspect <folder|session>", Short: "Show the container's networks, IP addresses, and gateways as JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		facts, err := e.NetworkFacts(cmd.Context(), args[0], *localName)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(facts)
	}}
	var key string
	env := &cobra.Command{Use: "env <folder|session>", Short: "Print network variables as shell export commands", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		facts, err := e.NetworkFacts(cmd.Context(), args[0], *localName)
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
	group.AddCommand(sessionNameFlag(inspect, localName), sessionNameFlag(env, localName))
	for _, action := range []string{"connect", "disconnect"} {
		action := action
		group.AddCommand(sessionNameFlag(&cobra.Command{Use: action + " <network> <folder|session>", Short: action + " an existing Docker network without changing saved configuration", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			e, err := factory(cmd)
			if err != nil {
				return err
			}
			return e.ChangeNetwork(cmd.Context(), args[1], *localName, args[0], action == "connect")
		}}, localName))
	}
	return group
}
