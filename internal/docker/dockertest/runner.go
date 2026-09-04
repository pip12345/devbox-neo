// Package dockertest provides a command-recording fake at the process boundary.
package dockertest

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"devbox/internal/docker"
)

type Step struct {
	Args   []string
	Output string
	Err    error
}
type Runner struct {
	mu    sync.Mutex
	Steps []Step
	Calls [][]string
}

func (r *Runner) Run(ctx context.Context, c docker.Command) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.Calls = append(r.Calls, slices.Clone(c.Args))
	if len(r.Steps) == 0 {
		return fmt.Errorf("unexpected Docker operation %s", c.Args[0])
	}
	step := r.Steps[0]
	r.Steps = r.Steps[1:]
	if !slices.Equal(step.Args, c.Args) {
		return fmt.Errorf("unexpected Docker arguments for %s", c.Args[0])
	}
	if c.Stdout != nil {
		if _, err := io.WriteString(c.Stdout, step.Output); err != nil {
			return err
		}
	}
	return step.Err
}
