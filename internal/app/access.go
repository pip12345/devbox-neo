package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func (e *Engine) readSession(ctx context.Context, name string) (store.Record, error) {
	r, err := e.Store.Read(ctx, name)
	if os.IsNotExist(err) {
		pending, pendingErr := e.Store.Pending(name)
		if pendingErr != nil {
			return r, pendingErr
		}
		if pending != nil {
			return r, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", name, err, pending.RetryStep())
		}
		err = commanderror.New("session_missing", "Session not found.", name, err,
			commanderror.Next("List sessions", "list"))
	}
	return r, err
}

// Locate resolves one requested identity, never a convenient inventory match.
// Exact names bypass configuration; folder selection reads only participation.
func (e *Engine) Locate(ctx context.Context, target, profile string) (store.Record, error) {
	if strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\") {
		r, err := e.readSession(ctx, target)
		if err == nil && ((profile != "" && profile != r.Identity.Profile) || (e.IgnoreProject && r.Identity.Project)) {
			return store.Record{}, fmt.Errorf("selection does not match the recorded session")
		}
		return r, err
	}
	id, err := environment.Select(e.Store.Home, target, profile, e.IgnoreProject, nil)
	if err != nil {
		return store.Record{}, err
	}
	return e.readSession(ctx, id.Name)
}
func (e *Engine) Start(ctx context.Context, target, profile string) (result Result, err error) {
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
	result = Result{Name: r.Identity.Name}
	started := false
	defer func() {
		if err != nil && started {
			err = errors.Join(err, e.stopUnattached(l, r))
		}
	}()
	c, started, err = e.startAccess(ctx, l, c, exists, &r, nil, &result)
	if err != nil {
		return result, err
	}
	if err = e.installRuntime(ctx, r); err != nil {
		return Result{}, err
	}
	r.Action = "start"
	r.Activity = time.Now().UTC()
	return result, e.saveManual(ctx, l, c, &r, true)
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
		return commanderror.New("container_missing", "Container not found.", r.Identity.Name, nil,
			commanderror.Next("Inspect session", "status", r.Identity.Name))
	}
	if c.State.Running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return err
		}
	}
	r.Action = "stop"
	r.Activity = time.Now().UTC()
	return e.saveManual(ctx, l, c, &r, false)
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
		return commanderror.New("container_missing", "Container not found.", r.Identity.Name, nil,
			commanderror.Next("Start or restore, then retry", "start", r.Identity.Name))
	}
	started := false
	defer func() {
		if err != nil && started && l.Held() {
			err = errors.Join(err, e.stopUnattached(l, r))
		}
	}()
	result := Result{Name: r.Identity.Name}
	c, started, err = e.startAccess(ctx, l, c, exists, &r, nil, &result)
	if err != nil {
		return err
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
