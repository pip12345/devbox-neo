package cli

import (
	"fmt"
	"os"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

var Version = "dev"

func New() *cobra.Command {
	var home, profile string
	root := &cobra.Command{Use: "devbox-neo", Short: "Persistent development environments", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&home, "home", "", "Devbox home (default ~/.devbox-neo; DEVBOX_HOME overrides)")
	root.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Select a profile slot")
	initialize := func(cmd *cobra.Command) (*store.Store, error) {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		resolved, err := config.Home(home, os.Getenv("DEVBOX_HOME"), userHome)
		if err != nil {
			return nil, err
		}
		return store.Open(cmd.Context(), resolved)
	}
	engine := func(cmd *cobra.Command) (*app.Engine, error) {
		state, err := initialize(cmd)
		if err != nil {
			return nil, err
		}
		tty := false
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			tty = terminal(f)
		}
		return &app.Engine{Store: state, Docker: docker.Runtime{Runner: docker.ExecRunner{}}, Streams: docker.Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), TTY: tty}, TerminalEnv: app.TerminalEnv(os.LookupEnv), UID: os.Getuid(), GID: os.Getgid()}, nil
	}
	var resume bool
	var openFlags creationFlags
	open := &cobra.Command{Use: "open <target> [-- harness-args...]", Short: "Open a configured target and launch its harness", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("use -- before one-off harness arguments")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		q := openFlags.Request(cmd, args[0], profile)
		q.Continue = resume
		q.Args = args[1:]
		_, err = e.Open(cmd.Context(), q)
		return err
	}}
	openFlags.Bind(open)
	open.Flags().BoolVarP(&resume, "continue", "c", false, "Continue the recorded harness session")
	root.AddCommand(open)
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
	var image, recreateAll bool
	var recreateFlags creationFlags
	recreate := &cobra.Command{Use: "recreate [target]", Short: "Explicitly apply current creation settings, preserving session state", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		if recreateAll {
			if len(args) > 0 {
				return fmt.Errorf("--all does not accept an exact target")
			}
			_, err = e.RecreateAll(cmd.Context(), image, recreateFlags.Request(cmd, "", profile))
			return err
		}
		if len(args) != 1 {
			return fmt.Errorf("provide a target or --all")
		}
		r, err := e.Locate(cmd.Context(), args[0], profile)
		if err != nil {
			return err
		}
		q := recreateFlags.Request(cmd, r.Identity.Workspace, r.Identity.Profile)
		q.ExpectedName = r.Identity.Name
		_, err = e.Recreate(cmd.Context(), q, image)
		return err
	}}
	recreateFlags.Bind(recreate)
	recreate.Flags().BoolVar(&image, "image", false, "Force a no-cache build (does not promise refreshed upstream bases)")
	recreate.Flags().BoolVar(&recreateAll, "all", false, "Recreate all selected owned containers after complete preflight")
	root.AddCommand(recreate)
	root.AddCommand(containerCommands(engine, &profile)...)
	root.AddCommand(sessionCommands(engine, &profile))
	root.AddCommand(resourceCommands(func(cmd *cobra.Command) (*resource.Service, error) {
		state, err := initialize(cmd)
		if err != nil {
			return nil, err
		}
		return &resource.Service{Home: state.Home}, nil
	})...)
	return root
}
