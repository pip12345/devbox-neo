package app

import (
	"context"
	"errors"

	"devbox/internal/docker"
	"devbox/internal/sshshare"
)

// Only normalize the foreground runner's result, before joining any connection
// or lease cleanup failures. In Docker raw mode Ctrl-C reaches the container as
// SIGINT (exit 130) without cancelling the host context. Host cancellation can
// instead terminate the runner with SIGTERM/SIGKILL. Neither is a login failure.
func sshRunResult(ctx context.Context, err error) error {
	if err == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var remaining []error
		for _, cause := range joined.Unwrap() {
			remaining = append(remaining, sshRunResult(ctx, cause))
		}
		return errors.Join(remaining...)
	}
	cancelled := errors.Is(ctx.Err(), context.Canceled)
	if cancelled && err == context.Canceled {
		return nil
	}
	code := 0
	var dockerExit *docker.ExitError
	var sshExit *sshshare.ExitError
	switch {
	case errors.As(err, &dockerExit):
		if dockerExit.Operation != "exec" {
			return err
		}
		code = dockerExit.Code
	case errors.As(err, &sshExit):
		code = sshExit.Code
	}
	if code == 130 || cancelled && (code == 137 || code == 143) {
		return nil
	}
	return err
}
