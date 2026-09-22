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

// FolderSessions is a saved-state query, independent of Docker and config
// resolution. Repair and default selection must not require a working daemon.
func (e *Engine) FolderSessions(ctx context.Context, folder string) (string, []store.Entry, error) {
	workspace, err := environment.CanonicalWorkspace(folder)
	if err != nil {
		return "", nil, err
	}
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return "", nil, err
	}
	var selected []store.Entry
	for _, entry := range entries {
		if entry.Record.Identity.Workspace == workspace {
			selected = append(selected, entry)
		}
	}
	slices.SortFunc(selected, func(a, b store.Entry) int {
		if a.Record.Identity.LocalName < b.Record.Identity.LocalName {
			return -1
		}
		if a.Record.Identity.LocalName > b.Record.Identity.LocalName {
			return 1
		}
		return 0
	})
	return workspace, selected, nil
}

func (e *Engine) SetDefault(ctx context.Context, selected store.Record) error {
	lock, err := e.Store.Lock(ctx, selected.Identity.Name)
	if err != nil {
		return err
	}
	defer lock.Close()
	return lock.SelectDefault(selected.ID)
}

func (e *Engine) ClearDefault(ctx context.Context, target string) (string, error) {
	workspace := ""
	if environment.IsSessionTarget(target) {
		r, err := e.readSession(ctx, target)
		if err != nil {
			return "", err
		}
		workspace = r.Identity.Workspace
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
	if err := config.ValidateReferenceChain(shown.Identity.Workspace, sources); err != nil {
		return store.Record{}, err
	}
	lock, err := e.Store.Lock(ctx, shown.Identity.Name)
	if err != nil {
		return store.Record{}, err
	}
	defer lock.Close()
	current, err := loadSelected(lock, shown)
	if err != nil {
		return store.Record{}, err
	}
	if !reflect.DeepEqual(current.Sources, shown.Sources) {
		return store.Record{}, commanderror.New("sources_changed", "Config sources changed while the editor was open; review them and retry.", shown.Identity.Name, nil)
	}
	current.Sources = slices.Clone(sources)
	return current, lock.Save(current)
}

func (e *Engine) CombinedConfiguration(r store.Record) (artifact.Resolved, error) {
	sources, err := config.ResolveReferences(r.Identity.Workspace, r.Sources)
	if err != nil {
		return artifact.Resolved{}, err
	}
	return artifact.Resolve(sources, config.Snapshot())
}
