package app

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Locate chooses saved identity before resolving configuration. A folder needs
// its explicit local name or saved default; inventory never supplies a fallback.
func (e *Engine) Locate(ctx context.Context, target, localName string) (store.Record, error) {
	if environment.IsSessionTarget(target) {
		r, err := e.readSession(ctx, target)
		if err == nil && localName != "" && localName != r.Identity.LocalName {
			return store.Record{}, fmt.Errorf("local name does not match the recorded session")
		}
		return r, err
	}
	workspace, err := environment.CanonicalWorkspace(target)
	if err != nil {
		return store.Record{}, commanderror.New("workspace_unavailable", "Cannot access workspace: "+err.Error(), target, err)
	}
	if localName != "" {
		if err := environment.ValidateLocalName(localName); err != nil {
			return store.Record{}, err
		}
		r, err := e.readSession(ctx, environment.ContainerName(workspace, localName))
		if err != nil {
			var actionable *commanderror.Error
			if errors.As(err, &actionable) && actionable.Code == "session_missing" {
				return r, creationRequired(target, localName, err)
			}
			return r, err
		}
		if r.Identity.Workspace != workspace || r.Identity.LocalName != localName {
			return store.Record{}, fmt.Errorf("saved session does not match the requested workspace and name")
		}
		return r, nil
	}
	selected, err := e.Store.ReadDefault(ctx, workspace)
	if err != nil {
		return store.Record{}, err
	}
	if selected == nil {
		entries, err := e.Store.Inventory(ctx)
		if err != nil {
			return store.Record{}, err
		}
		for _, entry := range entries {
			if entry.Err == nil && entry.Record.Identity.Workspace == workspace {
				return store.Record{}, commanderror.New("default_missing", "No default session selected.", target, nil,
					commanderror.Next("Select a default session", "set", target),
					commanderror.Next("Then open it", "open", target))
			}
		}
		return store.Record{}, commanderror.New("sessions_missing", "No sessions for this folder.", target, nil,
			commanderror.Next("Create a session", "create", target),
			commanderror.Next("Then select a default", "set", target),
			commanderror.Next("Then open it", "open", target))
	}
	r, err := e.readSession(ctx, selected.Name)
	if err != nil {
		var actionable *commanderror.Error
		if !errors.As(err, &actionable) || actionable.Code != "session_missing" {
			return r, err
		}
	}
	if err != nil || r.ID != selected.ID || r.Identity.Workspace != workspace {
		return store.Record{}, commanderror.New("default_unavailable", "The saved default session is unavailable. Select a default again.", target, err,
			commanderror.Next("Select a default session", "set", target))
	}
	return r, nil
}

// Selection is an invocation snapshot, including for folder defaults. Reload
// under the operation lock without adopting a replacement that reused the name.
func loadSelected(lock *store.Locked, selected store.Record) (store.Record, error) {
	current, err := lock.Load()
	if err != nil {
		return current, err
	}
	if current.ID != selected.ID || current.Identity != selected.Identity {
		return store.Record{}, commanderror.New("session_changed", "Selected session was replaced; select it again.", selected.Identity.Name, nil)
	}
	return current, nil
}

func (e *Engine) Start(ctx context.Context, target, localName string) (result Result, err error) {
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return Result{}, err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return Result{}, err
	}
	defer l.Close()
	r, err = loadSelected(l, r)
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
func (e *Engine) Stop(ctx context.Context, target, localName string, force bool) error {
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return err
	}
	defer l.Close()
	r, err = loadSelected(l, r)
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
func (e *Engine) Exec(ctx context.Context, target, localName string, argv []string, shell bool) (err error) {
	if _, err := store.ProcessIdentity(os.Getpid()); err != nil {
		return err
	}
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return err
	}
	l, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return err
	}
	defer l.Close()
	r, err = loadSelected(l, r)
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
