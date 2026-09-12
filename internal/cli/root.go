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
	root.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Use a named profile")
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
	var createFlags creationFlags
	create := &cobra.Command{Use: "create <folder>", Short: "Create a new environment and leave it stopped without launching its harness", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Create(cmd.Context(), createFlags.Request(cmd, args[0], profile))
		return err
	}}
	createFlags.Bind(create)
	root.AddCommand(create)
	var resume bool
	var onExit string
	var harnessArgs []string
	open := &cobra.Command{Use: "open <target> [-- harness-args...]", Short: "Open an existing environment and launch its harness", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("use -- before one-off harness arguments")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		q := app.Request{Workspace: args[0], Profile: profile, Continue: resume, Args: args[1:], Overrides: config.Layer{HarnessArgs: harnessArgs}}
		if cmd.Flags().Changed("on-exit") {
			q.Overrides.OnExit = &onExit
		}
		_, err = e.Open(cmd.Context(), q)
		return err
	}}
	open.Flags().StringVar(&onExit, "on-exit", "", "After the last command exits: stop (stop container) or running (leave running)")
	open.Flags().StringArrayVar(&harnessArgs, "harness-arg", nil, "Pass an argument to the harness (repeatable)")
	open.Flags().BoolVarP(&resume, "continue", "c", false, "Continue the previous harness session")
	root.AddCommand(open)
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { cmd.Println(Version); return nil }})
	root.AddCommand(&cobra.Command{Use: "start <target>", Short: "Start an existing container or restore it from saved session settings", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Start(cmd.Context(), args[0], profile)
		return err
	}})
	var force bool
	stop := &cobra.Command{Use: "stop <target>", Short: "Stop a Devbox container", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Stop(cmd.Context(), args[0], profile, force)
	}}
	stop.Flags().BoolVar(&force, "force", false, "Stop even if commands are still running")
	root.AddCommand(stop)
	root.AddCommand(&cobra.Command{Use: "shell <target>", Short: "Open a shell in an existing container", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], profile, nil, true)
	}})
	root.AddCommand(&cobra.Command{Use: "exec <target> -- <argv...>", Short: "Run a command in an existing container", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
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
	recreate := &cobra.Command{Use: "recreate [target]", Short: "Recreate the container with current settings, keeping session data", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	recreate.Flags().BoolVar(&image, "image", false, "Rebuild the image without using the build cache")
	recreate.Flags().BoolVar(&recreateAll, "all", false, "Recreate all Devbox containers, add --profile NAME to recreate all belonging to one profile")
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
	bindCompletionScripts(root)
	bindCompletions(root, docker.Runtime{Runner: docker.ExecRunner{}})
	bindCommandErrors(root)
	return root
}
