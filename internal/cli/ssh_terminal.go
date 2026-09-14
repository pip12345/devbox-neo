package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Docker changes the shared host terminal while its interactive exec is alive.
// These are only Devbox's messages; child SSH/Docker streams stay untouched.
func sshTerminalMessage(out io.Writer, text string) {
	if file, ok := out.(*os.File); ok {
		state, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
		if err == nil && (state.Oflag&unix.OPOST == 0 || state.Oflag&unix.ONLCR == 0) {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
	}
	fmt.Fprint(out, text)
}

func runSSHInTerminal(input *os.File, out io.Writer, run func() error) (err error) {
	state, err := unix.IoctlGetTermios(int(input.Fd()), unix.TCGETS)
	if err != nil {
		return fmt.Errorf("cannot capture SSH terminal settings: %w", err)
	}
	defer func() {
		// A cancelled Docker CLI can exit before its own restoration defer runs.
		// Restore before printing completion or handing an error to the CLI renderer.
		if restoreErr := unix.IoctlSetTermios(int(input.Fd()), unix.TCSETS, state); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("cannot restore SSH terminal settings: %w", restoreErr))
		}
		if err == nil {
			sshTerminalMessage(out, "\nDisconnected.\n")
		} else {
			sshTerminalMessage(out, "\n")
		}
	}()
	return run()
}
