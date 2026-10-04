package cli

import (
	"fmt"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func recreateCommand(factory engineFactory, localName *string) *cobra.Command {
	var image, container, all bool
	cmd := &cobra.Command{Use: "recreate [folder|session-id]", Short: "Apply current config, replacing the container or image only when needed", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
			_, err = e.RecreateAll(cmd.Context(), image, app.Request{LocalName: *localName, ForceContainer: container})
			return err
		}
		q := app.Request{Workspace: args[0], LocalName: *localName, ForceContainer: container}
		_, err = e.Recreate(cmd.Context(), q, image)
		return err
	}}
	cmd.Flags().BoolVar(&image, "image", false, "Force an uncached image rebuild and container replacement")
	cmd.Flags().BoolVar(&container, "container", false, "Force container replacement, reusing a compatible image when available")
	cmd.Flags().BoolVar(&all, "all", false, "Apply current config to all managed containers")
	return sessionNameFlag(cmd, localName)
}
