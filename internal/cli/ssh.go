package cli

import (
	"context"
	"fmt"
	"io"
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

func prepareSSH(in io.Reader, out io.Writer, destination string, hostMaster bool) (*os.File, error) {
	if err := sshshare.Validate(destination); err != nil {
		return nil, err
	}
	input, ok := in.(*os.File)
	if !ok || input == nil || !terminal(input) {
		return nil, fmt.Errorf("ssh requires an interactive host terminal")
	}
	if hostMaster {
		warning := hostMasterWarning
		if terminalColors(out).paint.Enabled() {
			warning = "\x1b[33m" + warning + "\x1b[0m"
		}
		fmt.Fprintln(out, warning+"\n")
	}
	return input, nil
}

func sshInteraction(ctx context.Context, input *os.File, out io.Writer, e *app.Engine, target, localName, destination string, hostMaster bool) error {
	fmt.Fprintf(out, "Connecting to %s…\n", destination)
	mode := "CONTAINER MASTER"
	if hostMaster {
		mode = "HOST MASTER"
	}
	return runSSHInTerminal(input, out, func() error {
		return e.SSH(ctx, target, localName, destination, app.SSHOptions{HostMaster: hostMaster, Connected: func(environment, alias string) {
			sshTerminalMessage(out, fmt.Sprintf("\nConnected: %s — %s\nShared with: %s\n\nInside the container:\n  ssh -F /devbox/ssh/config %s\n\nKeep this terminal open. Ctrl-C ends the shared connection\nand its active SSH sessions.\n", destination, mode, environment, alias))
		}})
	})
}
func sshCommand(factory engineFactory, localName *string) *cobra.Command {
	var hostMaster bool
	cmd := &cobra.Command{Use: "ssh <folder|session> <destination>", Short: "Share a user-authenticated SSH connection with an environment", Long: "Run a foreground SSH master inside the environment. Authenticate in this terminal, then let container processes reuse /devbox/ssh/config. Keep the terminal open; Ctrl-C disconnects. SSH configuration, including keys, ProxyJump, agent forwarding and X11 forwarding, comes from where the master runs. No keys or host SSH configuration are copied.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		input, err := prepareSSH(cmd.InOrStdin(), cmd.ErrOrStderr(), args[1], hostMaster)
		if err != nil {
			return err
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		return sshInteraction(cmd.Context(), input, cmd.ErrOrStderr(), e, args[0], *localName, args[1], hostMaster)
	}}
	cmd.Flags().BoolVar(&hostMaster, "host-master", false, "Run SSH on the host using host config and credentials; permits host-side forwarding through the shared control socket")
	return sessionNameFlag(cmd, localName)
}
