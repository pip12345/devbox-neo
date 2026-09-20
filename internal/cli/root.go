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
	var ignoreProject bool
	root := &cobra.Command{Use: "devbox-neo", Short: "Persistent development environments", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&home, "home", "", "Devbox home (default ~/.devbox-neo; DEVBOX_HOME overrides)")
	root.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Select the base profile")
	root.PersistentFlags().BoolVar(&ignoreProject, "ignore-project", false, "Exclude project configuration and artifacts")
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
		return &app.Engine{Store: state, Docker: docker.Runtime{Runner: docker.ExecRunner{}}, Streams: docker.Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), TTY: tty}, TerminalEnv: app.TerminalEnv(os.LookupEnv), IgnoreProject: ignoreProject, UID: os.Getuid(), GID: os.Getgid()}, nil
	}
	create := &cobra.Command{Use: "create <folder>", Short: "Create a new environment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Create(cmd.Context(), app.Request{Workspace: args[0], Profile: profile})
		return err
	}}
	root.AddCommand(create)
	var resume bool
	var harnessArgs []string
	open := &cobra.Command{Use: "open <folder|session> [-- harness-args...]", Short: "Open an existing environment and launch its harness", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("use -- before one-off harness arguments")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		q := app.Request{Workspace: args[0], Profile: profile, Continue: resume, Args: args[1:], Overrides: config.Layer{HarnessArgs: harnessArgs}}
		_, err = e.Open(cmd.Context(), q)
		return err
	}}
	open.Flags().StringArrayVar(&harnessArgs, "harness-arg", nil, "Pass an argument to the harness (repeatable)")
	open.Flags().BoolVarP(&resume, "continue", "c", false, "Continue the previous harness session")
	root.AddCommand(open)
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { cmd.Println(Version); return nil }})
	root.AddCommand(&cobra.Command{Use: "start <folder|session>", Short: "Keep a session running until stop, including across reboots", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Start(cmd.Context(), args[0], profile)
		return err
	}})
	var force bool
	stop := &cobra.Command{Use: "stop <folder|session>", Short: "Stop a session and clear its keep-running intent", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Stop(cmd.Context(), args[0], profile, force)
	}}
	stop.Flags().BoolVar(&force, "force", false, "Stop even if commands are still running")
	root.AddCommand(stop)
	root.AddCommand(&cobra.Command{Use: "shell <folder|session>", Short: "Open a shell in a session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], profile, nil, true)
	}})
	root.AddCommand(&cobra.Command{Use: "exec <folder|session> -- <argv...>", Short: "Run a command in a session", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("exec requires -- after its target")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], profile, args[1:], false)
	}})
	root.AddCommand(recreateCommand(engine, &profile))
	root.AddCommand(sshCommand(engine, &profile))
	root.AddCommand(containerCommands(engine, &profile)...)
	root.AddCommand(sessionCommands(engine, &profile)...)
	root.AddCommand(resourceCommands(func(cmd *cobra.Command) (*resource.Service, error) {
		state, err := initialize(cmd)
		if err != nil {
			return nil, err
		}
		return &resource.Service{Home: state.Home, IgnoreProject: ignoreProject, SelectedProfile: profile}, nil
	})...)
	bindCompletionScripts(root)
	bindCompletions(root, docker.Runtime{Runner: docker.ExecRunner{}})
	bindCommandErrors(root)
	return root
}
