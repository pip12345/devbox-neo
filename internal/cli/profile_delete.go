package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func profileDelete(factory resourceFactory) *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{Use: "delete <name>", Short: "Delete profile configuration without deleting containers or session state", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !force {
			if !interactive(cmd) || asJSON {
				return fmt.Errorf("profile deletion requires --force outside an interactive terminal")
			}
			selected, err := chooseOne(promptReader(cmd), cmd.OutOrStdout(), "Delete this profile and its source artifacts?", []string{"cancel", "delete"})
			if err != nil {
				return err
			}
			if selected != "delete" {
				cmd.Println("Cancelled.")
				return nil
			}
		}
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		result, err := service.DeleteProfile(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		result.Next = scopedSteps(cmd, result.Next, service.Home)
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		cmd.Printf("Deleted profile %s. Containers, sessions, and global defaults are unchanged.\n\nNext:\n%s", args[0], stepsText(result.Next))
		return nil
	}}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Delete without prompting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the result and next steps as JSON")
	return cmd
}
