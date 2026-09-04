package cli

import (
	"fmt"
	"os"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

var Version = "dev"

func New() *cobra.Command {
	var home, profile, network, onExit string
	var resume, readOnly bool
	root := &cobra.Command{Use: "devbox-neo <target> [-- harness-args...]", Short: "Persistent development environments (scratch rewrite)", SilenceUsage: true, SilenceErrors: true, Args: cobra.MinimumNArgs(1)}
	root.PersistentFlags().StringVar(&home, "home", "", "Devbox home (default ~/.devbox-neo; DEVBOX_HOME overrides)")
	root.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Select a profile slot")
	root.Flags().StringVar(&network, "network", "", "Primary Docker network")
	root.Flags().StringVar(&onExit, "on-exit", "", "After the last attached command: stop or running")
	root.Flags().BoolVarP(&resume, "continue", "c", false, "Continue the recorded harness session")
	root.Flags().BoolVar(&readOnly, "read-only", false, "Mount the workspace read-only at creation")
	engine := func(cmd *cobra.Command) (*app.Engine, error) {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		resolved, err := config.Home(home, os.Getenv("DEVBOX_HOME"), userHome)
		if err != nil {
			return nil, err
		}
		state, err := store.Open(cmd.Context(), resolved)
		if err != nil {
			return nil, err
		}
		tty := false
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			tty = terminal(f)
		}
		return &app.Engine{Store: state, Docker: docker.Runtime{Runner: docker.ExecRunner{}}, Streams: docker.Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), TTY: tty}, UID: os.Getuid(), GID: os.Getgid()}, nil
	}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("use -- before one-off harness arguments")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		q := app.Request{Workspace: args[0], Profile: profile, Continue: resume, ReadOnly: readOnly, Args: args[1:]}
		if cmd.Flags().Changed("network") {
			q.Overrides.Network = &network
		}
		if cmd.Flags().Changed("on-exit") {
			q.Overrides.OnExit = &onExit
		}
		_, err = e.Open(cmd.Context(), q)
		return err
	}
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { cmd.Println(Version); return nil }})
	root.AddCommand(&cobra.Command{Use: "start <target>", Short: "Start using recorded settings, or create a brand-new configured target", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Start(cmd.Context(), args[0], profile)
		return err
	}})
	var force bool
	stop := &cobra.Command{Use: "stop <target>", Short: "Stop an owned container", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Stop(cmd.Context(), args[0], profile, force)
	}}
	stop.Flags().BoolVar(&force, "force", false, "Permit disruption of attached Devbox commands")
	root.AddCommand(stop)
	root.AddCommand(&cobra.Command{Use: "shell <target>", Short: "Use the recorded shell without loading desired configuration", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], profile, nil, true)
	}})
	root.AddCommand(&cobra.Command{Use: "exec <target> -- <argv...>", Short: "Run argv in an existing container without loading desired configuration", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("exec requires -- after its target")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], profile, args[1:], false)
	}})
	var image bool
	recreate := &cobra.Command{Use: "recreate <target>", Short: "Explicitly apply current creation settings, preserving session state", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		r, err := e.Locate(cmd.Context(), args[0], profile)
		if err != nil {
			return err
		}
		_, err = e.Recreate(cmd.Context(), app.Request{Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, ExpectedName: r.Identity.Name}, image)
		return err
	}}
	recreate.Flags().BoolVar(&image, "image", false, "Force a no-cache build (does not promise refreshed upstream bases)")
	root.AddCommand(recreate)
	return root
}
