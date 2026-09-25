package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"devbox/internal/commanderror"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

type configUsersResult struct {
	Path     string   `json:"path"`
	Users    []string `json:"users"`
	Complete bool     `json:"complete"`
}

func configShowCommand(factory resourceFactory) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "show <name|path>", Short: "Show one config over built-in defaults, with source provenance", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, owner, err := inspectConfigOwner(cmd, factory, args[0])
		if err != nil {
			return err
		}
		view, err := s.ShowOwner(owner)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
		}
		return printConfigView(cmd.OutOrStdout(), view)
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print settings and provenance as JSON")
	return cmd
}

func configUsersCommand(factory resourceFactory) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "users <name|path>", Short: "List saved sessions using a config directory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, owner, err := inspectConfigOwner(cmd, factory, args[0])
		if err != nil {
			return err
		}
		users, scanErr := s.ConfigUsers(cmd.Context(), owner)
		result := configUsersResult{Path: owner.Root, Users: []string{}, Complete: scanErr == nil}
		for _, user := range users {
			result.Users = append(result.Users, user.Session)
		}
		if scanErr != nil {
			err = commanderror.New("config_usage_unknown", "Config usage report is incomplete: "+scanErr.Error(), owner.Root, scanErr,
				commanderror.Next("Inspect session inventory", "list"))
			// Known users remain useful when another saved record cannot be read.
			if !asJSON {
				err = errors.Join(err, printConfigUsers(cmd.OutOrStdout(), result))
			}
			return &partialResultError{cause: err, result: result}
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return printConfigUsers(cmd.OutOrStdout(), result)
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print users and scan completeness as JSON")
	return cmd
}

func inspectConfigOwner(cmd *cobra.Command, factory resourceFactory, input string) (*resource.Service, resource.Owner, error) {
	s, err := factory(cmd)
	if err != nil {
		return nil, resource.Owner{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, resource.Owner{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, resource.Owner{}, err
	}
	owner, err := s.ConfigDirectory(input, cwd, home)
	return s, owner, err
}

func printConfigUsers(out io.Writer, result configUsersResult) error {
	if _, err := fmt.Fprintf(out, "Config: %s\n", displayCell(result.Path)); err != nil {
		return err
	}
	for _, user := range result.Users {
		if _, err := fmt.Fprintf(out, "  %s\n", displayCell(user)); err != nil {
			return err
		}
	}
	if !result.Complete {
		_, err := fmt.Fprintln(out, "Usage scan incomplete; the listed users may not be exhaustive.")
		return err
	}
	if len(result.Users) == 0 {
		_, err := fmt.Fprintln(out, "No saved sessions use this config.")
		return err
	}
	return nil
}
