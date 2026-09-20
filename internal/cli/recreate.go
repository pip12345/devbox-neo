package cli

import (
	"fmt"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func recreateCommand(factory engineFactory, profile *string) *cobra.Command {
	var image, all bool
	var projectDir string
	cmd := &cobra.Command{Use: "recreate [folder|session]", Short: "Recreate the container with current settings, keeping session data", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("project-dir") {
			if projectDir == "" {
				return fmt.Errorf("--project-dir requires a directory path")
			}
			if all {
				return fmt.Errorf("--project-dir requires a single target; it cannot be used with --all")
			}
		}
		if all && len(args) > 0 {
			return fmt.Errorf("--all does not accept an exact target")
		}
		if !all && len(args) != 1 {
			return fmt.Errorf("provide a target or --all")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if all {
			_, err = e.RecreateAll(cmd.Context(), image, app.Request{Profile: *profile})
			return err
		}
		r, err := e.Locate(cmd.Context(), args[0], *profile)
		if err != nil {
			return err
		}
		q := app.Request{Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, Recorded: &r.Identity, Sources: r.Sources, ProjectDir: projectDir}
		_, err = e.Recreate(cmd.Context(), q, image)
		return err
	}}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Change the saved project config directory; use the workspace's .devbox/ to clear the override")
	_ = cmd.MarkFlagDirname("project-dir")
	cmd.Flags().BoolVar(&image, "image", false, "Rebuild the image without using the build cache")
	cmd.Flags().BoolVar(&all, "all", false, "Recreate all Devbox containers, add --profile NAME to recreate all belonging to one profile")
	return cmd
}
