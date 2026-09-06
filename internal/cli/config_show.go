package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func configCommand(factory resourceFactory, scope string) *cobra.Command {
	var show, asJSON bool
	var selectedProfile string
	use := "config"
	args := cobra.NoArgs
	if scope == "profile" {
		use += " <name>"
		args = cobra.ExactArgs(1)
	}
	if scope == "project" {
		use += " <folder>"
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: "Show effective configuration and source provenance", Args: args, RunE: func(cmd *cobra.Command, args []string) error {
		if !show {
			return fmt.Errorf("interactive configuration editing is not implemented yet.\nUse --show to inspect the effective configuration.")
		}
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		var view resource.ConfigView
		switch scope {
		case "global":
			view, err = service.ShowGlobal()
		case "profile":
			view, err = service.ShowProfile(args[0])
		case "project":
			view, err = service.ShowProject(args[0], selectedProfile)
		}
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
		}
		cmd.Printf("%s configuration: %s\n", scope, view.Path)
		for _, layer := range view.Trace.Layers {
			cmd.Printf("  layer: %s", layer.Name)
			if layer.Path != "" {
				cmd.Printf(" (%s)", layer.Path)
			}
			cmd.Println()
		}
		for _, excluded := range view.Trace.Excluded {
			cmd.Printf("  excluded: %s\n", excluded)
		}
		keys := make([]string, 0, len(view.Values))
		for key := range view.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, _ := json.Marshal(view.Values[key])
			source := view.Trace.Sources[key]
			if len(source) == 0 {
				source = []string{"built-in default"}
			}
			cmd.Printf("%s = %s  [%s]\n", key, value, strings.Join(source, " -> "))
		}
		keys = nil
		for key := range view.Trace.Winners {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			cmd.Printf("%s: %s\n", key, view.Trace.Winners[key])
		}
		if view.Harness != nil {
			cmd.Printf("Harness: %s (%s)\n", view.Harness["name"], view.Harness["origin"])
		}
		return nil
	}}
	cmd.Flags().BoolVar(&show, "show", false, "Print effective values and provenance without prompting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print --show output as JSON")
	if scope == "project" {
		cmd.Flags().StringVar(&selectedProfile, "profile", "", "Select an explicit profile and exclude project artifacts")
	}
	return cmd
}
