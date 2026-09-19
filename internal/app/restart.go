package app

import (
	"context"
	"errors"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func restartPolicy(manual bool) string {
	if manual {
		return "unless-stopped"
	}
	return "no"
}

func (e *Engine) syncRestart(ctx context.Context, c docker.Container, r store.Record) error {
	return e.Docker.RestartPolicy(ctx, c, e.owner(r), restartPolicy(r.ManualStart))
}

// The session owns keep-running intent; Docker enforces it when no CLI is alive.
// If record publication fails, restore the old policy rather than reporting a
// failed operation that silently changes behavior across the next reboot.
func (e *Engine) saveManual(ctx context.Context, l *store.Locked, c docker.Container, r *store.Record, manual bool) error {
	old := r.ManualStart
	if err := e.Docker.RestartPolicy(ctx, c, e.owner(*r), restartPolicy(manual)); err != nil {
		return err
	}
	r.ManualStart = manual
	if err := l.Save(*r); err != nil {
		r.ManualStart = old
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.HostConfig.RestartPolicy.Name = restartPolicy(manual)
		return errors.Join(err, e.syncRestart(cleanup, c, *r))
	}
	return nil
}
