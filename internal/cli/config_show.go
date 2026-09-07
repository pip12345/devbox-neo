package cli

import (
	"encoding/json"
	"fmt"

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
	cmd := &cobra.Command{Use: use, Short: "Edit local settings or show effective configuration and sources", Args: args, RunE: func(cmd *cobra.Command, args []string) error {
		if !show && asJSON {
			return fmt.Errorf("--json requires --show")
		}
		if !show && scope == "project" && cmd.Flags().Changed("profile") {
			return fmt.Errorf("--profile requires --show; the interactive menu edits the project's own configuration")
		}
		if !show && !interactive(cmd) {
			return fmt.Errorf("configuration menus require a terminal.\nUse --show to inspect configuration without prompting.")
		}
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		if !show {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}
			owner, err := service.ConfigOwner(scope, target)
			if err != nil {
				return err
			}
			return runConfigMenu(cmd, service, owner)
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
		return printConfigView(cmd.OutOrStdout(), view)
	}}
	cmd.Flags().BoolVar(&show, "show", false, "Print effective values and provenance without prompting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print --show output as JSON")
	if scope == "project" {
		cmd.Flags().StringVar(&selectedProfile, "profile", "", "Select an explicit profile and exclude project artifacts")
	}
	return cmd
}
