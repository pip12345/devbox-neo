package app

import (
	"context"
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
	known := map[string]bool{}
	for _, entry := range entries {
		if entry.Record.Applied.SetupContainer != "" {
			known[entry.Record.Applied.SetupContainer] = true
		}
	}
	displayEntries := []store.Entry{}
	for _, entry := range entries {
		if p := entry.Pending; p != nil && p.Mode == "relocate" {
			authority := p.Source
			if p.Phase == "committed" {
				authority = p.Destination
			}
			if entry.Name != authority {
				continue
			}
		}
		displayEntries = append(displayEntries, entry)
	}
	for _, view := range e.inventoryViews(displayEntries, live, true) {
		if workspace == "" || view.Workspace == workspace {
			report.Sessions = append(report.Sessions, view)
		}
	}
	for _, container := range live {
		if known[container.ID] {
			continue
		}
		folder := container.Config.Labels[docker.Namespace+".workspace"]
		if workspace != "" && folder != workspace {
			continue
		}
		name := strings.TrimPrefix(container.Name, "/")
		pending, _ := e.Store.PendingID(container.Config.Labels[docker.Namespace+".session"])
		report.UnmatchedContainers = append(report.UnmatchedContainers, View{Target: name, OwnerSessionID: container.Config.Labels[docker.Namespace+".session"], ContainerName: name, ContainerID: container.ID, Workspace: folder, Exists: true, Running: container.State.Running, Pending: pending, Error: "container has no valid durable association"})
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
		if selected := defaults[view.Workspace]; selected != nil && selected.ID == view.SessionID {
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
	images, err := e.Docker.ImageIDs(ctx)
	if err != nil {
		return report, err
	}
	records := map[string]store.Record{}
	for _, entry := range entries {
		if entry.Err == nil && entry.Record.ID != "" {
			records[entry.Record.ID] = entry.Record
		}
	}
	for i := range report.Sessions {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		view := &report.Sessions[i]
		if r, ok := records[view.SessionID]; ok && view.Error == "" && view.Pending == nil {
			view.ImageMissing = !images[r.Applied.ImageID]
			e.desiredStatus(view, r)
		}
	}
	return report, nil
}
