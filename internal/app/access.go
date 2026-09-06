package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// Locate uses only durable identity, never desired config or the harness registry.
func (e *Engine) Locate(ctx context.Context, target, profile string) (store.Record, error) {
	if strings.HasPrefix(target, docker.Namespace+"-") && !strings.ContainsAny(target, "/\\") {
		return e.Store.Read(ctx, target)
	}
	id, err := environment.Identify(target, "", true)
	if err != nil {
		return store.Record{}, err
	}
	if profile != "" {
		id, err := environment.Identify(target, profile, false)
		if err != nil {
			return store.Record{}, err
		}
		return e.Store.Read(ctx, id.Name)
	}
	entries, err := os.ReadDir(filepath.Join(e.Store.Home, "sessions"))
	if err != nil {
		return store.Record{}, err
	}
	var matches []store.Record
	for _, entry := range entries {
		if !entry.IsDir() {
			return store.Record{}, fmt.Errorf("unexpected non-directory in session inventory")
		}
		r, err := e.Store.Read(ctx, entry.Name())
		if err != nil {
			return store.Record{}, err
		}
		if r.Identity.Workspace == id.Workspace && (profile == "" || r.Identity.Profile == profile) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return store.Record{}, os.ErrNotExist
	}
	if len(matches) > 1 {
		return store.Record{}, fmt.Errorf("workspace has multiple recorded slots.\nSelect one with --profile or use its exact container name.")
	}
	return matches[0], nil
}
func (e *Engine) Start(ctx context.Context, target, profile string) (Result, error) {
	r, err := e.Locate(ctx, target, profile)
	if os.IsNotExist(err) {
		s, err := e.Resolve(Request{Workspace: target, Profile: profile})
		if err != nil {
			return Result{}, err
		}
		l, err := e.Store.Lock(ctx, s.Identity.Name)
		if err != nil {
			return Result{}, err
		}
		defer l.Close()
		if _, err = l.Load(); !os.IsNotExist(err) {
			if err == nil {
				return Result{}, fmt.Errorf("session was created concurrently.\nRetry the start command.")
			}
			return Result{}, err
		}
		if err = e.requireNew(ctx, l); err != nil {
			return Result{}, err
		}
		_, _, err = e.create(ctx, l, s, nil, false)
		return Result{Name: s.Identity.Name}, err
	}
	if err != nil {
		return Result{}, err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return Result{}, err
	}
	defer l.Close()
	r, err = l.Load()
	if err != nil {
		return Result{}, err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		c, err = e.recover(ctx, l, &r, nil)
		if err != nil {
			return Result{}, err
		}
	}
	if !c.State.Running {
		if err = e.start(ctx, c, r); err != nil {
			return Result{}, err
		}
	}
	if err = e.installRuntime(ctx, r); err != nil {
		return Result{}, err
	}
	r.Action = "start"
	r.Activity = time.Now().UTC()
	return Result{Name: r.Identity.Name}, l.Save(r)
}
func (e *Engine) Stop(ctx context.Context, target, profile string, force bool) error {
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return err
	}
	defer l.Close()
	r, err = l.Load()
	if err != nil {
		return err
	}
	if !force {
		if err = l.RequireIdle(); err != nil {
			return err
		}
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("container is missing.\n\nInspect its session state:\n  devbox-neo session show %s", r.Identity.Name)
	}
	if c.State.Running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return err
		}
	}
	r.Action = "stop"
	r.Activity = time.Now().UTC()
	return l.Save(r)
}
func (e *Engine) Exec(ctx context.Context, target, profile string, argv []string, shell bool) (err error) {
	if _, err := store.ProcessIdentity(os.Getpid()); err != nil {
		return err
	}
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return err
	}
	defer l.Close()
	r, err = l.Load()
	if err != nil {
		return err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("shell and exec require an existing container.\n\nNext:\n  devbox-neo start %s\nThen retry your command.", r.Identity.Name)
	}
	started := false
	defer func() {
		if err != nil && started && l.Held() {
			err = errors.Join(err, e.stopUnattached(l, r))
		}
	}()
	if !c.State.Running {
		if err = e.start(ctx, c, r); err != nil {
			return err
		}
		c.State.Running = true
		started = true
	}
	if err = e.installRuntime(ctx, r); err != nil {
		return err
	}
	action := "exec"
	if shell {
		argv = append([]string(nil), r.Launch.Shell...)
		action = "shell"
	}
	r.Action = action
	r.Activity = time.Now().UTC()
	if err = l.Save(r); err != nil {
		return err
	}
	return e.attach(ctx, l, c, r, action, argv)
}
