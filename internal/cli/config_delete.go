package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func configDeleteCommand(factory resourceFactory) *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{Use: "delete <name>", Short: "Delete an unreferenced named config and its files", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !force && (!interactive(cmd) || asJSON) {
			return fmt.Errorf("config delete requires --force without a terminal or with --json")
		}
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		path, err := service.CheckConfigDeletion(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if !force {
			fmt.Fprintf(cmd.OutOrStdout(), "Delete config %s and all files in %s? [y/N] ", displayCell(args[0]), displayCell(path))
			line, err := promptReader(cmd).ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}
			answer := strings.ToLower(strings.TrimSpace(line))
			if answer != "y" && answer != "yes" {
				cmd.Println("Cancelled.")
				return nil
			}
		}
		result, err := service.DeleteConfig(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		cmd.Printf("Deleted config %s (%s).\n", displayCell(args[0]), displayCell(path))
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation; never bypass saved-session reference checks")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the deletion result as JSON; requires --force")
	return cmd
}
