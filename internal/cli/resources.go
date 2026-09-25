package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"devbox/internal/commanderror"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

type resourceFactory func(*cobra.Command) (*resource.Service, error)

func configCommands(factory resourceFactory) *cobra.Command {
	group := &cobra.Command{Use: "config", Short: "Create, edit, list, and delete config directories"}
	group.AddCommand(directoryCommand(factory, true), directoryCommand(factory, false), configListCommand(factory), configDeleteCommand(factory))
	return group
}

func configListCommand(factory resourceFactory) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Short: "List named configs in the selected home, including invalid ones", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		configs, err := service.ListConfigs()
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(configs)
		}
		if err := writeListTitle(cmd.OutOrStdout(), displayCell(filepath.Join(service.Home, "configs"))); err != nil {
			return err
		}
		if len(configs) == 0 {
			cmd.Println("No named configs.")
			cmd.Print(stepsText(scopedSteps(cmd, []commanderror.Step{commanderror.Next("Create a config", "config", "create", "base")}, service.Home)))
			return nil
		}
		table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "NAME\tHARNESS\tPATH")
		for _, item := range configs {
			fmt.Fprintf(table, "%s\t%s\t%s\n", displayCell(item.Name), displayCell(item.Harness), displayCell(item.Path))
		}
		if err := table.Flush(); err != nil {
			return err
		}
		for _, item := range configs {
			if item.Error != "" {
				cmd.Printf("! %s: %s\n", displayCell(item.Name), displayCell(item.Error))
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print named config entries as JSON")
	return cmd
}

func directoryCommand(factory resourceFactory, create bool) *cobra.Command {
	use, description := "edit <name|path>", "Edit a config directory or add missing optional files"
	validateArgs := cobra.ExactArgs(1)
	if create {
		use, description = "create [name|path]", "Create a config directory and offer initial setup"
		validateArgs = cobra.MaximumNArgs(1)
	}
	var selected, artifactHarness string
	var artifacts []string
	var asJSON bool
	cmd := &cobra.Command{Use: use, Short: description, Args: validateArgs, RunE: func(cmd *cobra.Command, args []string) error {
		direct := cmd.Flags().Changed("harness") || cmd.Flags().Changed("artifact") || cmd.Flags().Changed("artifact-harness") || asJSON
		input := ""
		if len(args) > 0 {
			input = args[0]
		} else if direct || !interactive(cmd) {
			return fmt.Errorf("config create requires a name or path outside interactive setup")
		}
		service, err := factory(cmd)
		if err != nil {
			return err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		options := resource.SetupOptions{Artifacts: artifacts, ArtifactHarness: artifactHarness}
		if cmd.Flags().Changed("harness") {
			options.Harness = &selected
		}
		if create && !direct && interactive(cmd) {
			m := newMenu(cmd)
			_, result, created, createErr := createConfig(m, service, input, cwd, userHome)
			if finishErr := m.Finish(); finishErr != nil {
				return errors.Join(createErr, finishErr)
			}
			if (errors.Is(createErr, io.EOF) && len(result.Created) == 0) || (createErr == nil && !created) {
				cmd.Println("Cancelled. No config was created.")
				return nil
			}
			return renderResource(cmd, result, createErr, asJSON, service.Home)
		}
		owner, err := service.ConfigDirectory(input, cwd, userHome)
		if err != nil {
			return err
		}
		if create {
			result, err := service.CreateConfig(cmd.Context(), owner, options)
			return renderResource(cmd, result, err, asJSON, service.Home)
		}
		if !direct && interactive(cmd) {
			if _, err := service.ConfigSource(owner); err != nil {
				return err
			}
			writeMenuHint(cmd.OutOrStdout(), "Directory: "+displayCell(owner.Root))
			users, reportErr := service.ConfigUsers(cmd.Context(), owner)
			if len(users) > 0 {
				writeMenuHint(cmd.OutOrStdout(), "Used by saved sessions:")
				for _, user := range users {
					writeMenuHint(cmd.OutOrStdout(), "  "+displayCell(user.Session))
				}
			}
			if reportErr != nil {
				writeMenuHint(cmd.OutOrStdout(), "Shared-use report is incomplete: "+displayCell(reportErr.Error()))
			}
			return runConfigMenu(cmd, service, owner)
		}
		result, err := service.EditConfig(cmd.Context(), owner, options)
		if err == nil && !asJSON && (len(result.Created) > 0 || len(result.Updated) > 0) {
			result.Next = []commanderror.Step{commanderror.Next("Review pending changes", "status")}
		}
		return renderResource(cmd, result, err, asJSON, service.Home)
	}}
	cmd.Flags().StringVar(&selected, "harness", "", "Set this config's persistent harness selection")
	cmd.Flags().StringSliceVar(&artifacts, "artifact", nil, "Add missing harness-config, setup.sh, before-open.sh, or Dockerfile files (repeatable)")
	cmd.Flags().StringVar(&artifactHarness, "artifact-harness", "", "Choose which harness's files to add without changing the config's harness")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the operation result as JSON; never prompt")
	return cmd
}

func interactive(cmd *cobra.Command) bool {
	file, ok := cmd.InOrStdin().(*os.File)
	return ok && terminal(file)
}

func renderResource(cmd *cobra.Command, result resource.Result, operationErr error, asJSON bool, home string) error {
	result.Next = scopedSteps(cmd, result.Next, home)
	if asJSON {
		if operationErr != nil {
			return operationErr
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", displayCell(warning))
	}
	if operationErr == nil {
		cmd.Printf("Configured %s\n", displayCell(result.Path))
	}
	for _, path := range result.Created {
		cmd.Printf("Created %s\n", displayCell(path))
	}
	for _, path := range result.Updated {
		cmd.Printf("Updated %s\n", displayCell(path))
	}
	for _, path := range result.Skipped {
		cmd.Printf("Kept existing %s\n", displayCell(path))
	}
	if operationErr != nil {
		return operationErr
	}
	if len(result.Next) > 0 {
		cmd.Printf("\n%s", stepsText(result.Next))
	}
	return nil
}
