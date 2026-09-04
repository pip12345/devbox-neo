// Package docker translates typed runtime operations to Docker CLI commands.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
		if code < 1 {
			code = 1
		}
		return &ExitError{Code: code, Operation: operation}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("cannot execute Docker CLI: %w", err)
}
