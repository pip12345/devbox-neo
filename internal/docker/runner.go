// Package docker translates typed runtime operations to Docker CLI commands.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"devbox/internal/commanderror"
)

type Command struct {
	Args   []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Runner interface {
	Run(context.Context, Command) error
}

type ExecRunner struct{ Binary string }

// ExitError preserves foreground exit status without exposing secret-bearing argv.
type ExitError struct {
	Code      int
	Operation string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("docker %s failed (exit %d)", e.Operation, e.Code)
}

func (r ExecRunner) Run(ctx context.Context, c Command) error {
	binary := r.Binary
	if binary == "" {
		binary = "docker"
	}
	cmd := exec.CommandContext(ctx, binary, c.Args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Dir, c.Stdin, c.Stdout, c.Stderr
	// Docker plugins can inherit captured pipes. Cancellation must not wait
	// indefinitely for a descendant to close them after the CLI process exits.
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		operation := "command"
		if len(c.Args) > 0 {
			operation = c.Args[0]
		}
		code := exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}
		if code < 1 {
			code = 1
		}
		failure := &ExitError{Code: code, Operation: operation}
		return errors.Join(commanderror.New("docker_command_failed", failure.Error(), "", failure), ctx.Err())
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return commanderror.New("docker_unavailable", "cannot execute Docker CLI; check its installation, PATH, and executable permissions", binary, err)
}
