package app

import (
	"context"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/store"
)

// Access starts the recorded runtime, never applies desired configuration. Even
// repair shells must remain available when selected configs or live JSON break.
func (e *Engine) startAccess(ctx context.Context, lock *store.Locked, c docker.Container, exists bool, r store.Record) (docker.Container, bool, error) {
	if !exists {
		return c, false, commanderror.New("container_missing", "Container not found; recreate the session before accessing it.", r.Directory, nil,
			commanderror.Next("Recreate from current configuration", "recreate", r.Directory))
	}
	if err := checkDurableStores(lock, r); err != nil {
		return c, false, err
	}
	if _, err := lock.LiveLeases(); err != nil {
		return c, false, err
	}
	if c.State.Running {
		return c, false, e.syncRestart(ctx, c, r)
	}
	if err := e.start(ctx, c, r); err != nil {
		return c, false, err
	}
	c.State.Running = true
	return c, true, nil
}
