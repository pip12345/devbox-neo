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

func sessionNameFlag(cmd *cobra.Command, name *string) *cobra.Command {
	cmd.Flags().StringVar(name, "name", *name, "Select the session's folder-local name")
	return cmd
}

func New() *cobra.Command {
	var home, localName string
	root := &cobra.Command{Use: "devbox-neo", Short: "Persistent development environments", Long: "Persistent development environments\nRun without a subcommand in a terminal to browse sessions and configs.\nUse arrows and Enter, Tab to switch browsers, / to filter, and Esc to go back.\nExplicit commands and JSON output remain available for direct use and scripts.", Example: "  # First run\n  devbox-neo config create base\n  devbox-neo create .\n  devbox-neo edit .\n  devbox-neo open .", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&home, "home", "", "Devbox home (default ~/.devbox-neo; DEVBOX_HOME overrides)")
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
		if file, ok := cmd.InOrStdin().(*os.File); ok {
			tty = terminal(file)
		}
		return &app.Engine{Store: state, Docker: docker.Runtime{Runner: docker.ExecRunner{}}, Streams: docker.Streams{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), TTY: tty}, OnDiagnostic: diagnosticRenderer(cmd.ErrOrStderr()), TerminalEnv: app.TerminalEnv(os.LookupEnv), UID: os.Getuid(), GID: os.Getgid()}, nil
	}
	resources := func(cmd *cobra.Command) (*resource.Service, error) {
		state, err := initialize(cmd)
		if err != nil {
			return nil, err
		}
		return &resource.Service{Home: state.Home}, nil
	}
	root.AddCommand(createCommand(engine, &localName))
	var resume bool
	var harnessArgs []string
	open := &cobra.Command{Use: "open <folder|session-id> [-- harness-args...]", Short: "Open an existing session and launch its harness", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 && cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("use -- before one-off harness arguments")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Open(cmd.Context(), app.Request{Workspace: args[0], LocalName: localName, Continue: resume, Args: args[1:], HarnessArgs: harnessArgs})
		return err
	}}
	open.Flags().StringArrayVar(&harnessArgs, "harness-arg", nil, "Pass an argument to the harness (repeatable)")
	open.Flags().BoolVarP(&resume, "continue", "c", false, "Continue the previous harness session")
	root.AddCommand(sessionNameFlag(open, &localName))
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print the Devbox version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { cmd.Println(Version); return nil }})
	root.AddCommand(sessionNameFlag(&cobra.Command{Use: "start <folder|session-id>", Short: "Start and keep running until stop, including across reboots", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		_, err = e.Start(cmd.Context(), args[0], localName)
		return err
	}}, &localName))
	var force bool
	stop := &cobra.Command{Use: "stop <folder|session-id>", Short: "Stop a session and clear its keep-running intent", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Stop(cmd.Context(), args[0], localName, force)
	}}
	stop.Flags().BoolVar(&force, "force", false, "Stop even if commands are still running")
	root.AddCommand(sessionNameFlag(stop, &localName))
	root.AddCommand(sessionNameFlag(&cobra.Command{Use: "shell <folder|session-id>", Short: "Open a shell in a session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], localName, nil, true)
	}}, &localName))
	root.AddCommand(sessionNameFlag(&cobra.Command{Use: "exec <folder|session-id> -- <argv...>", Short: "Run a command in a session", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() != 1 {
			return fmt.Errorf("exec requires -- after its target")
		}
		e, err := engine(cmd)
		if err != nil {
			return err
		}
		return e.Exec(cmd.Context(), args[0], localName, args[1:], false)
	}}, &localName))
	root.AddCommand(recreateCommand(engine, &localName), sshCommand(engine, &localName))
	root.AddCommand(containerCommands(engine, &localName)...)
	root.AddCommand(sessionCommands(engine, &localName)...)
	configGroup := configCommands(resources)
	root.RunE = func(cmd *cobra.Command, _ []string) error { return runFrontend(cmd, engine, resources, false) }
	configGroup.RunE = func(cmd *cobra.Command, _ []string) error { return runFrontend(cmd, engine, resources, true) }
	root.AddCommand(configGroup, editCommand(engine, &localName))
	bindCompletionScripts(root)
	bindCompletions(root, docker.Runtime{Runner: docker.ExecRunner{}})
	bindCommandErrors(root)
	return root
}
