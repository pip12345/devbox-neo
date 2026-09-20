package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"devbox/internal/docker"
	"devbox/internal/store"
)

// Unmatched containers are diagnostics, not invented durable sessions.
type InventoryReport struct {
	Sessions            []View `json:"sessions"`
	UnmatchedContainers []View `json:"unmatched_containers"`
}

func (e *Engine) List(ctx context.Context, profile string) (InventoryReport, error) {
	entries, live, err := e.inventory(ctx)
	if err != nil {
		return InventoryReport{}, err
	}
	return e.sessionInventory(entries, live, profile), nil
}

func (e *Engine) sessionInventory(entries []store.Entry, live []docker.Container, profile string) InventoryReport {
	report := InventoryReport{Sessions: []View{}, UnmatchedContainers: []View{}}
	liveNames := map[string]bool{}
	for _, c := range live {
		liveNames[strings.TrimPrefix(c.Name, "/")] = true
	}
	retained := []store.Entry{}
	for _, entry := range entries {
		if liveNames[entry.Name] && errors.Is(entry.Err, os.ErrNotExist) && entry.Pending == nil {
			continue
		}
		retained = append(retained, entry)
	}
	entries = retained
	known := map[string]bool{}
	profiles := map[string]string{}
	projects := map[string]bool{}
	for _, entry := range entries {
		known[entry.Name] = true
		if entry.Err == nil {
			profiles[entry.Name] = entry.Record.Identity.Profile
			projects[entry.Name] = entry.Record.Identity.Project
		}
	}
	for _, c := range live {
		// Labels retain filtering for corrupt records; they never authorize mutations.
		name := strings.TrimPrefix(c.Name, "/")
		if _, recorded := profiles[name]; !recorded {
			profiles[name] = c.Config.Labels[docker.Namespace+".profile"]
			projects[name] = c.Config.Labels[docker.Namespace+".project"] == "true"
		}
	}
	for _, view := range e.inventoryViews(entries, live, true) {
		if (profile == "" || profiles[view.Name] == profile) && !(e.IgnoreProject && projects[view.Name]) {
			report.Sessions = append(report.Sessions, view)
		}
	}
	for _, view := range e.inventoryViews(entries, live, false) {
		if !known[view.Name] && (profile == "" || profiles[view.Name] == profile) && !(e.IgnoreProject && projects[view.Name]) {
			report.UnmatchedContainers = append(report.UnmatchedContainers, view)
		}
	}
	return report
}

// StatusAll checks saved environments even when their containers are missing.
// Broken records and pending transfers remain visible without guessed baselines.
func (e *Engine) StatusAll(ctx context.Context, profile string) (InventoryReport, error) {
	entries, live, err := e.inventory(ctx)
	if err != nil {
		return InventoryReport{}, err
	}
	report := e.sessionInventory(entries, live, profile)
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
