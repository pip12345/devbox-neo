package app

import (
	"context"
	"reflect"
	"slices"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func (e *Engine) SetDefault(ctx context.Context, selected store.Record) error {
	lock, err := e.Store.Lock(ctx, selected.Directory, selected.ID)
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := loadSelected(lock, selected)
	if err != nil {
		return err
	}
	if current.Settings.Workspace != selected.Settings.Workspace {
		return commanderror.New("workspace_changed", "Workspace changed; select the folder default again.", selected.Directory, nil)
	}
	return lock.SelectDefault(selected.ID)
}

func (e *Engine) ClearDefault(ctx context.Context, target string) (string, error) {
	workspace := ""
	if environment.IsSessionTarget(target) || environment.IsSessionID(target) {
		r, err := e.Locate(ctx, target, "")
		if err != nil {
			return "", err
		}
		workspace = r.Settings.Workspace
	} else {
		var err error
		workspace, err = environment.CanonicalWorkspace(target)
		if err != nil {
			return "", err
		}
	}
	return workspace, e.Store.ClearDefault(ctx, workspace)
}

// UpdateSources compares only durable identity and the desired chain displayed
// to the editor. Unrelated activity and applied inputs are reloaded and retained.
func (e *Engine) UpdateSources(ctx context.Context, shown store.Record, sources []config.Reference) (store.Record, error) {
	// Drafts may be empty, but a saved selection must be replaceable without
	// passing through an unconfigured state. This does not require runnable inputs.
	if len(sources) == 0 {
		return store.Record{}, commanderror.New("configs_required", "Select at least one config; replace the final config instead of removing it.", shown.Directory, nil)
	}
	if err := config.ValidateReferenceChain(shown.Settings.Workspace, sources); err != nil {
		return store.Record{}, err
	}
	lock, err := e.Store.Lock(ctx, shown.Directory, shown.ID)
	if err != nil {
		return store.Record{}, err
	}
	defer lock.Close()
	current, err := loadSelected(lock, shown)
	if err != nil {
		return store.Record{}, err
	}
	if current.Settings.Workspace != shown.Settings.Workspace || !reflect.DeepEqual(current.Settings.Sources, shown.Settings.Sources) {
		return store.Record{}, commanderror.New("sources_changed", "Selected configs changed; review the current selection and retry.", shown.Directory, nil)
	}
	current.Settings.Sources = slices.Clone(sources)
	return current, lock.Save(current)
}

func (e *Engine) CombinedConfiguration(r store.Record) (artifact.Resolved, error) {
	sources, err := config.ResolveReferences(r.Settings.Workspace, r.Settings.Sources)
	if err != nil {
		return artifact.Resolved{}, err
	}
	return artifact.Resolve(sources, config.Snapshot())
}
