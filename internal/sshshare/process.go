package sshshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// ExitError preserves SSH's exit status without including credential-bearing
// configuration or subprocess arguments in diagnostics.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("SSH failed (exit %d).", e.Code) }
func (e *ExitError) ExitCode() int { return e.Code }

func (c *Connection) MasterCommand(directory string, insecure bool) []string {
	argv := []string{"bash", "-c", Supervisor, "devbox-ssh", directory, c.Destination}
	if insecure {
		// Ignore stored keys as well as accepting new ones: OpenSSH otherwise
		// restricts authentication and forwarding when a saved key has changed.
		argv = append(argv, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "GlobalKnownHostsFile=/dev/null")
	}
	return argv
}

func RunHost(ctx context.Context, c *Connection, insecure bool, in io.Reader, out, stderr io.Writer) error {
	argv := c.MasterCommand(c.HostDirectory(), insecure)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
	// Let the supervisor terminate/reap its children on ordinary cancellation;
	// the owner-lock watchdog separately covers abrupt controller death.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}
		if code < 1 {
			code = 1
		}
		return errors.Join(&ExitError{Code: code}, ctx.Err())
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
