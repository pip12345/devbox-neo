package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// Unmatched containers and broken default selections remain diagnostics; they
// never become invented durable sessions or hide the records we can still read.
type InventoryReport struct {
	Sessions            []View            `json:"sessions"`
	UnmatchedContainers []View            `json:"unmatched_containers"`
	DefaultErrors       map[string]string `json:"default_errors,omitempty"`
}

func (e *Engine) List(ctx context.Context, folder string) (InventoryReport, error) {
	workspace := ""
	if folder != "" {
		var err error
		workspace, err = environment.CanonicalWorkspace(folder)
		if err != nil {
			return InventoryReport{}, err
		}
	}
	entries, live, err := e.inventory(ctx)
	if err != nil {
		return InventoryReport{}, err
	}
	return e.sessionInventory(ctx, entries, live, workspace), nil
}

func (e *Engine) sessionInventory(ctx context.Context, entries []store.Entry, live []docker.Container, workspace string) InventoryReport {
	report := InventoryReport{Sessions: []View{}, UnmatchedContainers: []View{}, DefaultErrors: map[string]string{}}
	liveNames := map[string]bool{}
	workspaces := map[string]string{}
	for _, container := range live {
		name := strings.TrimPrefix(container.Name, "/")
		liveNames[name] = true
		workspaces[name] = container.Config.Labels[docker.Namespace+".workspace"]
	}
	retained := []store.Entry{}
	known := map[string]bool{}
	for _, entry := range entries {
		if liveNames[entry.Name] && errors.Is(entry.Err, os.ErrNotExist) && entry.Pending == nil {
			continue
		}
		retained = append(retained, entry)
		known[entry.Name] = true
		if entry.Err == nil {
			workspaces[entry.Name] = entry.Record.Identity.Workspace
		}
	}
	for _, view := range e.inventoryViews(retained, live, true) {
		if workspace == "" || workspaces[view.Name] == workspace {
			report.Sessions = append(report.Sessions, view)
		}
	}
	for _, view := range e.inventoryViews(retained, live, false) {
		if !known[view.Name] && (workspace == "" || workspaces[view.Name] == workspace) {
			report.UnmatchedContainers = append(report.UnmatchedContainers, view)
		}
	}
	defaults := map[string]*store.DefaultSession{}
	readDefault := func(path string) {
		if _, read := defaults[path]; read || path == "" {
			return
		}
		selected, err := e.Store.ReadDefault(ctx, path)
		defaults[path] = selected
		if err != nil {
			report.DefaultErrors[path] = err.Error()
		}
	}
	readDefault(workspace)
	matched := map[string]bool{}
	for i := range report.Sessions {
		view := &report.Sessions[i]
		readDefault(view.Workspace)
		if selected := defaults[view.Workspace]; selected != nil && selected.Name == view.Name && selected.ID == view.SessionID {
			view.Default = true
			matched[view.Workspace] = true
		}
	}
	for path, selected := range defaults {
		if selected != nil && !matched[path] {
			report.DefaultErrors[path] = "saved default is unavailable; select a default again"
		}
	}
	return report
}

// StatusAll checks saved environments even when their containers are missing.
// Broken records and pending transfers remain visible without guessed baselines.
func (e *Engine) StatusAll(ctx context.Context, folder string) (InventoryReport, error) {
	workspace := ""
	if folder != "" {
		var err error
		workspace, err = environment.CanonicalWorkspace(folder)
		if err != nil {
			return InventoryReport{}, err
		}
	}
	entries, live, err := e.inventory(ctx)
	if err != nil {
		return InventoryReport{}, err
	}
	report := e.sessionInventory(ctx, entries, live, workspace)
	records := map[string]store.Record{}
	for _, entry := range entries {
		if entry.Err == nil && entry.Record.ID != "" {
			records[entry.Name] = entry.Record
		}
	}
	for i := range report.Sessions {
		if err := ctx.Err(); err != nil {
			return InventoryReport{}, err
		}
		view := &report.Sessions[i]
		if r, ok := records[view.Name]; ok && view.Error == "" && view.Pending == nil {
			e.desiredStatus(view, r)
		}
	}
	return report, nil
}
