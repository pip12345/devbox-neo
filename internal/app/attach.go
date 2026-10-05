package app

import (
	"context"
	"errors"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func (e *Engine) stopUnattached(l *store.Locked, r store.Record) error {
	if r.ID == "" || r.Settings.ManualStart {
		return nil
	}
	active, err := l.Active()
	if err != nil || len(active) != 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, exists, err := e.inspect(ctx, r)
	if err != nil || !exists || !c.State.Running {
		return err
	}
	return e.Docker.Stop(ctx, c, e.owner(r))
}

func (e *Engine) attach(ctx context.Context, l *store.Locked, c docker.Container, r store.Record, action string, argv []string) error {
	return e.attachRun(l, r, action, func() error {
		return e.Docker.Exec(ctx, c, e.owner(r), argv, e.TerminalEnv, e.Streams)
	})
}

// attachRun owns the lease for both container commands and foreground SSH
// controllers. A host-side master must protect the environment just as an exec does.
func (e *Engine) attachRun(l *store.Locked, r store.Record, action string, run func() error) (err error) {
	lease, err := l.Lease(action)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		lock, cleanupErr := e.Store.Lock(cleanup, r.Directory, r.ID)
		if cleanupErr == nil {
			defer lock.Close()
			var released bool
			released, cleanupErr = lock.Release(lease.ID)
			if cleanupErr == nil && released {
				current, touchErr := lock.Touch(r.ID, action)
				active, aerr := lock.Active()
				cleanupErr = errors.Join(touchErr, aerr)
				if current.ID != "" && aerr == nil && len(active) == 0 && !current.Settings.ManualStart {
					live, exists, ierr := e.inspect(cleanup, current)
					cleanupErr = errors.Join(cleanupErr, ierr)
					if ierr == nil && exists && live.State.Running {
						cleanupErr = errors.Join(cleanupErr, e.Docker.Stop(cleanup, live, e.owner(current)))
					}
				}
			}
		}
		// Joining keeps errors.As able to find the foreground ExitError. Cleanup is
		// still visible instead of replacing the user's command status with success.
		err = errors.Join(err, cleanupErr)
	}()
	if err = l.Close(); err != nil {
		return err
	}
	return run()
}
