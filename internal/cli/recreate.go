package cli

import (
	"fmt"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func recreateCommand(factory engineFactory, localName *string) *cobra.Command {
	var image, harness, container, force, all bool
	cmd := &cobra.Command{Use: "recreate [folder|session]", Short: "Apply current config, replacing the container or image only when needed", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if all && len(args) > 0 {
			return fmt.Errorf("--all does not accept an exact target")
		}
		if !all && len(args) != 1 {
			return fmt.Errorf("provide a target or --all")
		}
		if all && *localName != "" {
			return fmt.Errorf("--name requires an explicit folder target")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if all {
			_, err = e.RecreateAll(cmd.Context(), image, app.RecreateOptions{RefreshHarness: harness, ForceContainer: container, Force: force})
			return err
		}
		q := app.RecreateRequest{Target: args[0], LocalName: *localName, Options: app.RecreateOptions{RefreshHarness: harness, ForceContainer: container, Force: force}}
		_, err = e.Recreate(cmd.Context(), q, image)
		return err
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Replace the container even with attached commands; interrupts commands and loses container-local changes")
	cmd.Flags().BoolVar(&image, "image", false, "Force an uncached image rebuild and container replacement")
	cmd.Flags().BoolVar(&harness, "harness", false, "Refresh harness installation without cache and replace the container (earlier image stages may use cache)")
	cmd.Flags().BoolVar(&container, "container", false, "Force container replacement, reusing a compatible image when available")
	cmd.Flags().BoolVar(&all, "all", false, "Apply current config to all saved sessions, including missing containers")
	return sessionNameFlag(cmd, localName)
}
