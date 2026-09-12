package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func (e *Engine) readSession(ctx context.Context, name string) (store.Record, error) {
	r, err := e.Store.Read(ctx, name)
	if os.IsNotExist(err) {
		err = commanderror.New("session_missing", "no durable session was found", name, err,
			commanderror.Next("Find an existing session", "session", "list"))
	}
	return r, err
}

// Locate uses only durable identity, never desired config or the harness registry.
func (e *Engine) Locate(ctx context.Context, target, profile string) (store.Record, error) {
	if strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\") {
		return e.readSession(ctx, target)
	}
	id, err := environment.Identify(target, "", true)
	if err != nil {
		return store.Record{}, commanderror.New("workspace_unavailable", err.Error(), target, err)
	}
	if profile != "" {
		id, err := environment.Identify(target, profile, false)
		if err != nil {
			return store.Record{}, err
		}
		return e.readSession(ctx, id.Name)
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
		return store.Record{}, commanderror.New("session_missing", "no durable session was found for this workspace", id.Workspace, os.ErrNotExist,
			commanderror.Next("Find an existing session", "session", "list"))
	}
	if len(matches) > 1 {
		return store.Record{}, commanderror.New("ambiguous_target", "workspace has multiple recorded slots; select --profile or an exact container name", id.Workspace, nil,
			commanderror.Next("Find the exact session name", "session", "list"))
	}
	return matches[0], nil
}
func (e *Engine) Start(ctx context.Context, target, profile string) (Result, error) {
	r, err := e.Locate(ctx, target, profile)
	var missing *commanderror.Error
	if errors.As(err, &missing) && missing.Code == "session_missing" &&
		!(strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\")) {
		return Result{}, creationRequired(target, profile, err)
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
		return commanderror.New("container_missing", "container is missing", r.Identity.Name, nil,
			commanderror.Next("Inspect retained session state", "session", "show", r.Identity.Name))
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
		return commanderror.New("container_missing", "shell and exec require an existing container", r.Identity.Name, nil,
			commanderror.Next("Start or recover the recorded container, then retry", "start", r.Identity.Name))
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
