package app

import (
	"context"
	"time"

	"devbox/internal/environment"
	"devbox/internal/fsutil"
)

type RenameResult struct {
	SessionID    string `json:"session_id"`
	PreviousName string `json:"previous_name"`
	LocalName    string `json:"local_name"`
	Workspace    string `json:"workspace"`
	DryRun       bool   `json:"dry_run"`
}

// Rename edits the session's label, not its storage or applied container.
func (e *Engine) Rename(ctx context.Context, target, localName, to string, dryRun bool) (RenameResult, error) {
	var result RenameResult
	if err := environment.ValidateLocalName(to); err != nil {
		return result, err
	}
	names, err := e.Store.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(names)
	selected, err := e.Locate(ctx, target, localName)
	if err != nil {
		return result, err
	}
	lock, err := e.Store.Lock(ctx, selected.Directory, selected.ID)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	r, err := loadSelected(lock, selected)
	if err != nil {
		return result, err
	}
	next := r.Settings.Binding
	next.LocalName = to
	if err := e.Store.RequireUnusedBinding(ctx, next, r.Directory); err != nil {
		return result, err
	}
	result = RenameResult{SessionID: r.ID, PreviousName: r.Settings.LocalName, LocalName: to, Workspace: r.Settings.Workspace, DryRun: dryRun}
	if dryRun || r.Settings.LocalName == to {
		return result, nil
	}
	r.Settings.Binding = next
	r.Activity, r.Action = time.Now().UTC(), "rename"
	return result, lock.Save(r)
}
