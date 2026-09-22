package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

type resourceFactory func(*cobra.Command) (*resource.Service, error)

func configCommands(engine engineFactory, factory resourceFactory, localName *string) *cobra.Command {
	group := &cobra.Command{Use: "config", Short: "Create and edit config directories or manage a session's config sources"}
	group.AddCommand(directoryCommand(factory, true), directoryCommand(factory, false), sourcesCommand(engine, localName))
	return group
}

func directoryCommand(factory resourceFactory, create bool) *cobra.Command {
	action, description := "edit", "Edit a config directory or add missing optional files"
	if create {
		action, description = "create", "Create a config directory and offer initial setup"
	}
	var selected, artifactHarness string
	var artifacts []string
	var asJSON bool
	cmd := &cobra.Command{Use: action + " <reference>", Short: description, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		owner, err := service.ConfigDirectory(args[0], cwd, userHome)
		if err != nil {
			return err
		}
		options := resource.SetupOptions{Artifacts: artifacts, ArtifactHarness: artifactHarness}
		if cmd.Flags().Changed("harness") {
			options.Harness = &selected
		}
		direct := cmd.Flags().Changed("harness") || cmd.Flags().Changed("artifact") || cmd.Flags().Changed("artifact-harness") || asJSON
		if create {
			if err := service.CheckConfigCreation(owner); err != nil {
				return err
			}
			if !direct && interactive(cmd) {
				m := menu{ctx: cmd.Context(), in: promptReader(cmd), out: cmd.OutOrStdout()}
				var proceed bool
				options, proceed, err = configCreationMenu(m, service.Home)
				if errors.Is(err, io.EOF) || (err == nil && !proceed) {
					cmd.Println("Cancelled. No config was created.")
					return nil
				}
				if err != nil {
					return err
				}
			}
			result, err := service.CreateConfig(cmd.Context(), owner, options)
			return renderResource(cmd, result, err, asJSON, service.Home)
		}
		if !direct && interactive(cmd) {
			if _, err := service.ConfigSource(owner); err != nil {
				return err
			}
			writeMenuHint(cmd.OutOrStdout(), "Directory: "+displayCell(owner.Root))
			names, reportErr := service.ReferencingSessions(cmd.Context(), owner)
			if len(names) > 0 {
				writeMenuHint(cmd.OutOrStdout(), "Referenced by saved sessions:")
				for _, name := range names {
					writeMenuHint(cmd.OutOrStdout(), "  "+displayCell(name))
				}
			}
			if reportErr != nil {
				writeMenuHint(cmd.OutOrStdout(), "Shared-use report is incomplete: "+displayCell(reportErr.Error()))
			}
			return runConfigMenu(cmd, service, owner)
		}
		result, err := service.EditConfig(cmd.Context(), owner, options)
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
