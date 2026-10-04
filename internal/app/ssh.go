package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

type SSHOptions struct {
	HostMaster bool
	// Connected is called only after authentication and client-config publication.
	Connected func(environment, alias string)
}

func (e *Engine) SSH(ctx context.Context, target, localName, destination string, options SSHOptions) (err error) {
	if err = sshshare.Validate(destination); err != nil {
		return err
	}
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return err
	}
	l, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		return err
	}
	defer l.Close()
	r, err = loadSelected(l, r)
	if err != nil {
		return err
	}
	root, err := l.Path(sshshare.RelativeRoot)
	if err != nil {
		return err
	}
	mounted := false
	for _, m := range r.Applied.Creation.Mounts {
		if m.Target == sshshare.Mount && m.Source == root && !m.ReadOnly {
			mounted = true
		}
	}
	if !mounted {
		return commanderror.New("ssh_mount_missing", "This environment needs recreation before SSH sharing is available.", r.ID, nil, commanderror.Next("Recreate", "recreate", r.ID))
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	started := false
	defer func() {
		if err != nil && started && l.Held() {
			err = errors.Join(err, e.stopUnattached(l, r))
		}
	}()
	c, started, err = e.startAccess(ctx, l, c, exists, r)
	if err != nil {
		return err
	}
	if err = e.refreshNetwork(ctx, r); err != nil {
		return err
	}
	connection, err := sshshare.Prepare(root, destination)
	if err != nil {
		return err
	}
	// This defer also covers lease creation/unlock failures before the runner owns
	// the connection. Close is idempotent and never leaves the owner lock held.
	defer func() { err = errors.Join(err, connection.Close()) }()
	r.Action = "ssh"
	r.Activity = time.Now().UTC()
	if err = l.Save(r); err != nil {
		return err
	}
	return e.attachRun(l, r, "ssh", func() (runErr error) {
		// Revoke SSH before releasing its environment lease can trigger automatic shutdown.
		defer func() { runErr = errors.Join(runErr, connection.Close()) }()
		return e.runSSH(ctx, c, r, connection, options)
	})
}

func (e *Engine) runSSH(ctx context.Context, c docker.Container, r store.Record, connection *sshshare.Connection, options SSHOptions) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		if options.HostMaster {
			done <- sshshare.RunHost(runCtx, connection, e.Streams.In, e.Streams.Out, e.Streams.Err)
		} else {
			argv := []string{"bash", "-c", sshshare.Supervisor, "devbox-ssh", connection.ContainerDirectory(), connection.Destination}
			done <- e.Docker.Exec(runCtx, c, e.owner(r), argv, e.TerminalEnv, e.Streams)
		}
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	connected := false
	nextInspect := time.Now().Add(time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				return sshRunResult(ctx, err)
			}
			if !connected {
				return fmt.Errorf("SSH exited before a shared connection became available")
			}
			return err
		case <-ctx.Done():
			cancel()
			runErr := <-done
			return sshRunResult(ctx, errors.Join(ctx.Err(), runErr))
		case <-ticker.C:
			if !connected && connection.Ready() {
				if err := connection.Publish(); err != nil {
					cancel()
					runErr := <-done
					return errors.Join(err, sshRunResult(runCtx, runErr))
				}
				connected = true
				if options.Connected != nil {
					options.Connected(r.ID, connection.Alias)
				}
			}
			if time.Now().Before(nextInspect) {
				continue
			}
			nextInspect = time.Now().Add(time.Second)
			// Forced stop/deletion or lost Docker contact must also end a host master;
			// its TCP connection otherwise has no dependency on the container lifetime.
			// Keep this short read-only probe independent of terminal cancellation:
			// interrupting the Docker inspection must not turn Ctrl-C into an
			// unrelated "Docker inspect failed" error. Cancellation resumes below
			// after this bounded probe; real inspection failures remain visible.
			inspectCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			current, exists, err := e.inspect(inspectCtx, r)
			stop()
			if err != nil || !exists || !current.State.Running || current.ID != c.ID {
				cancel()
				runErr := <-done
				return errors.Join(fmt.Errorf("SSH sharing ended because the environment is no longer available"), err, sshRunResult(runCtx, runErr))
			}
		}
	}
}
