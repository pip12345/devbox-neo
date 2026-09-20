package cli

import (
	"fmt"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func recreateCommand(factory engineFactory, profile *string) *cobra.Command {
	var image, all bool
	cmd := &cobra.Command{Use: "recreate [folder|session]", Short: "Recreate the container with current settings, keeping session data", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		q := app.Request{Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, Recorded: &r.Identity, Sources: r.Sources}
		_, err = e.Recreate(cmd.Context(), q, image)
		return err
	}}
	cmd.Flags().BoolVar(&image, "image", false, "Rebuild the image without using the build cache")
	cmd.Flags().BoolVar(&all, "all", false, "Recreate all Devbox containers, add --profile NAME to recreate all belonging to one profile")
	return cmd
}
