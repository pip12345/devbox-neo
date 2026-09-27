package app

import (
	"context"
	"time"

	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// SetWorkspace changes desired settings only. The recorded runtime remains the
// authority for stopping and replacing the old container, even if its mount is gone.
func (e *Engine) SetWorkspace(ctx context.Context, target, localName, workspace string) (store.Record, error) {
	canonical, err := environment.CanonicalWorkspace(workspace)
	if err != nil {
		return store.Record{}, err
	}
	names, err := e.Store.LockNames(ctx)
	if err != nil {
		return store.Record{}, err
	}
	defer fsutil.Unlock(names)
	selected, err := e.Locate(ctx, target, localName)
	if err != nil {
		return store.Record{}, err
	}
	lock, err := e.Store.Lock(ctx, selected.Directory, selected.ID)
	if err != nil {
		return store.Record{}, err
	}
	defer lock.Close()
	r, err := loadSelected(lock, selected)
	if err != nil {
		return r, err
	}
	next := r.Settings.Binding
	next.Workspace = canonical
	if err := e.Store.RequireUnusedBinding(ctx, next, r.Directory); err != nil {
		return r, err
	}
	if next == r.Settings.Binding {
		return r, nil
	}
	if err := lock.ClearMatchingDefault(ctx, r.Settings.Workspace, r.ID); err != nil {
		return r, err
	}
	r.Settings.Binding = next
	r.Activity, r.Action = time.Now().UTC(), "edit-workspace"
	return r, lock.Save(r)
}
