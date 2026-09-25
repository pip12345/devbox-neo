package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

// This only controls presentation. CheckConfigDeletion remains the authority
// for directory kind, symlinks, current users, and incomplete inventories.
func isNamedConfig(name string) bool {
	return filepath.IsLocal(name) && name != "." && name != "~" && filepath.Base(name) == name
}
func deleteConfigWorkflow(m menu, s *resource.Service, name string, force bool) (resource.Result, bool, error) {
	path, err := s.CheckConfigDeletion(m.Context, name)
	if err != nil {
		return resource.Result{}, false, err
	}
	if !force {
		confirmed, err := m.Confirm(fmt.Sprintf("Delete config %s and all files in %s? [y/N] ", displayCell(name), displayCell(path)))
		if err != nil || !confirmed {
			return resource.Result{}, !confirmed, err
		}
	}
	result, err := s.DeleteConfig(m.Context, name)
	return result, false, err
}
func configDeleteCommand(factory resourceFactory) *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{Use: "delete <name>", Short: "Delete an unreferenced named config and its files", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		if !force && (!interactive(cmd) || asJSON) {
			return fmt.Errorf("config delete requires --force without a terminal or with --json")
		}
		s, err := factory(cmd)
		if err != nil {
			return err
		}
		m := newMenu(cmd)
		defer func() { runErr = errors.Join(runErr, m.Finish()) }()
		result, cancelled, err := deleteConfigWorkflow(m, s, args[0], force)
		if err != nil {
			return err
		}
		if cancelled {
			cmd.Println("Cancelled.")
			return nil
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		cmd.Printf("Deleted config %s (%s).\n", displayCell(args[0]), displayCell(result.Path))
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation; never bypass saved-session reference checks")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the deletion result as JSON; requires --force")
	return cmd
}
