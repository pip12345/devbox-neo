package cli

import (
	"fmt"
	"os"

	"devbox/internal/app"
	"devbox/internal/sshshare"
	"github.com/spf13/cobra"
)

const hostMasterWarning = `WARNING: Running in host SSH master mode

The container can act as your authenticated SSH user on the remote
server. It can also create tunnels into your computer and networks
your computer can reach, potentially exposing private services
and data outside the container.`

func sshCommand(factory engineFactory, profile *string) *cobra.Command {
	var hostMaster bool
	cmd := &cobra.Command{
		Use:   "ssh <target> <destination>",
		Short: "Share a user-authenticated SSH connection with an environment",
		Long:  "Run a foreground SSH master inside the environment. Authenticate in this terminal, then let container processes reuse /devbox/ssh/config. Keep the terminal open; Ctrl-C disconnects. SSH configuration, including keys, ProxyJump, agent forwarding and X11 forwarding, comes from where the master runs. No keys or host SSH configuration are copied.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sshshare.Validate(args[1]); err != nil {
				return err
			}
			input, ok := cmd.InOrStdin().(*os.File)
			if !ok || !terminal(input) {
				return fmt.Errorf("ssh requires an interactive host terminal")
			}
			if hostMaster {
				warning := hostMasterWarning
				if terminalColors(cmd.ErrOrStderr()).enabled {
					warning = "\x1b[33m" + warning + "\x1b[0m"
				}
				fmt.Fprintln(cmd.ErrOrStderr(), warning+"\n")
			}
			e, err := factory(cmd)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Connecting to %s…\n", args[1])
			mode := "CONTAINER MASTER"
			if hostMaster {
				mode = "HOST MASTER"
			}
			return runSSHInTerminal(input, cmd.ErrOrStderr(), func() error {
				return e.SSH(cmd.Context(), args[0], *profile, args[1], app.SSHOptions{HostMaster: hostMaster, Connected: func(environment, alias string) {
					sshTerminalMessage(cmd.ErrOrStderr(), fmt.Sprintf("\nConnected: %s — %s\nShared with: %s\n\nInside the container:\n  ssh -F /devbox/ssh/config %s\n\nKeep this terminal open. Ctrl-C ends the shared connection\nand its active SSH sessions.\n", args[1], mode, environment, alias))
				}})
			})
		},
	}
	cmd.Flags().BoolVar(&hostMaster, "host-master", false, "Run SSH on the host using host config and credentials; permits host-side forwarding through the shared control socket")
	return cmd
}
