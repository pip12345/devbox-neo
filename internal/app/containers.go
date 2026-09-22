package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

type View struct {
	Name                string                    `json:"name"`
	Workspace           string                    `json:"workspace,omitempty"`
	LocalName           string                    `json:"local_name,omitempty"`
	Default             bool                      `json:"default"`
	Sources             []config.Reference        `json:"sources,omitempty"`
	ManualStart         bool                      `json:"manual_start"`
	Harness             string                    `json:"harness,omitempty"`
	SessionID           string                    `json:"session_id,omitempty"`
	LastActivity        time.Time                 `json:"last_activity,omitempty"`
	LastAction          string                    `json:"last_action,omitempty"`
	CreatedAt           time.Time                 `json:"created_at,omitempty"`
	ContainerID         string                    `json:"container_id,omitempty"`
	Exists              bool                      `json:"exists"`
	Running             bool                      `json:"running"`
	Error               string                    `json:"error,omitempty"`
	Desired             environment.Change        `json:"desired_change,omitempty"`
	PendingInputChanges []environment.InputChange `json:"pending_input_changes,omitempty"`
	ConfigError         string                    `json:"config_error,omitempty"`
	Pending             *store.Reservation        `json:"pending_transfer,omitempty"`
}

func recordView(r store.Record) View {
	return View{Name: r.Identity.Name, Workspace: r.Identity.Workspace, LocalName: r.Identity.LocalName, Sources: r.Sources, ManualStart: r.ManualStart, Harness: r.Definition.Name, SessionID: r.ID, LastActivity: r.Activity, LastAction: r.Action, CreatedAt: r.Created}
}
func (e *Engine) inventory(ctx context.Context) ([]store.Entry, []docker.Container, error) {
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return nil, nil, err
	}
	live, err := e.Docker.Inventory(ctx, e.Store.Installation)
	return entries, live, err
}

func (e *Engine) inventoryViews(entries []store.Entry, live []docker.Container, sessions bool) []View {
	records := map[string]store.Entry{}
	for _, entry := range entries {
		records[entry.Name] = entry
	}
	result := []View{}
	for _, container := range live {
		name := strings.TrimPrefix(container.Name, "/")
		entry, found := records[name]
		if sessions && !found {
			continue
		}
		view := View{Name: name, ContainerID: container.ID, Exists: true, Running: container.State.Running}
		if found && entry.Err == nil && entry.Record.ID != "" {
			view = recordView(entry.Record)
			view.ContainerID = container.ID
			view.Exists = true
			view.Running = container.State.Running
			if err := container.Verify(e.owner(entry.Record)); err != nil {
				view.Error = err.Error()
			} else if container.ID != entry.Record.SetupContainer || container.Image != entry.Record.ImageID {
				view.Error = "container differs from committed creation contract"
			}
		} else if found && entry.Err != nil {
			view.Error = entry.Err.Error()
		} else if !found || entry.Pending == nil {
			view.Error = "container has no durable record"
		}
		view.CreatedAt = container.Created
		view.Pending = entry.Pending
		result = append(result, view)
		delete(records, name)
	}
	if sessions {
		for name, entry := range records {
			view := recordView(entry.Record)
			view.Name = name
			view.Pending = entry.Pending
			if entry.Err != nil {
				view.Error = entry.Err.Error()
			}
			result = append(result, view)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (e *Engine) desiredStatus(view *View, r store.Record) {
	desired, err := e.Resolve(Request{Workspace: r.Identity.Workspace, LocalName: r.Identity.LocalName, Recorded: &r.Identity, Sources: r.Sources})
	if err != nil {
		view.ConfigError = err.Error()
	} else {
		report := environment.CompareInputs(r.Inputs, desired.Inputs)
		view.Desired, view.PendingInputChanges = report.Change, report.PendingInputChanges
	}
}
func (e *Engine) Logs(ctx context.Context, target, localName string, follow bool, tail string) error {
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return commanderror.New("container_missing", "Container not found; its logs are unavailable.", r.Identity.Name, nil,
			commanderror.Next("Inspect session", "status", r.Identity.Name))
	}
	return e.Docker.Logs(ctx, c, e.owner(r), follow, tail, e.Streams.Out, e.Streams.Err)
}

type Selection struct {
	Targets     []string
	LocalName   string
	All         bool
	Stopped     bool
	selectedIDs map[string]string
}

func (e *Engine) selectContainers(ctx context.Context, selection *Selection) ([]string, error) {
	if (selection.All && selection.Stopped) || ((selection.All || selection.Stopped) && len(selection.Targets) > 0) {
		return nil, fmt.Errorf("use exact targets, --all, or --stopped, not a combination")
	}
	if selection.LocalName != "" && (selection.All || selection.Stopped) {
		return nil, fmt.Errorf("--name requires an explicit folder target")
	}
	if selection.selectedIDs == nil {
		selection.selectedIDs = map[string]string{}
	}
	names := map[string]bool{}
	if selection.All || selection.Stopped {
		containers, err := e.Docker.Inventory(ctx, e.Store.Installation)
		if err != nil {
			return nil, err
		}
		for _, c := range containers {
			if !selection.Stopped || !c.State.Running {
				names[strings.TrimPrefix(c.Name, "/")] = true
			}
		}
	} else {
		if len(selection.Targets) == 0 {
			return nil, fmt.Errorf("provide a target or an explicit selection flag")
		}
		for _, target := range selection.Targets {
			r, err := e.Locate(ctx, target, selection.LocalName)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) && strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\") {
					names[target] = true
					selection.selectedIDs[target] = ""
					continue
				}
				return nil, err
			}
			names[r.Identity.Name] = true
			selection.selectedIDs[r.Identity.Name] = r.ID
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
func (e *Engine) DeleteContainers(ctx context.Context, selection Selection, force bool) ([]string, error) {
	names, err := e.selectContainers(ctx, &selection)
	if err != nil {
		return nil, err
	}
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return nil, err
	}
	defer store.CloseAll(locks)
	return e.deleteContainersLocked(ctx, selection, locks, force, false)
}

func (e *Engine) deleteContainersLocked(ctx context.Context, selection Selection, locks []*store.Locked, force, dryRun bool) ([]string, error) {
	var err error
	type removal struct {
		record    store.Record
		container docker.Container
		lock      *store.Locked
		owner     docker.Owner
	}
	removals := []removal{}
	for _, lock := range locks {
		r, loadErr := lock.Load()
		if loadErr != nil && !os.IsNotExist(loadErr) {
			return nil, loadErr
		}
		if expected, tracked := selection.selectedIDs[lock.Name]; tracked && r.ID != expected {
			return nil, fmt.Errorf("selected session changed; retry deletion")
		}
		if !force {
			if err = lock.RequireIdle(); err != nil {
				return nil, err
			}
		}
		var c docker.Container
		var exists bool
		var owner docker.Owner
		if loadErr == nil {
			c, exists, err = e.inspect(ctx, r)
			owner = e.owner(r)
		} else {
			c, exists, err = e.Docker.Inspect(ctx, lock.Name)
			if err == nil && exists {
				owner, err = e.orphanOwner(c)
			}
		}
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if selection.LocalName != "" && owner.LocalName != selection.LocalName {
			return nil, fmt.Errorf("local name does not match the selected container")
		}
		if selection.Stopped && c.State.Running {
			return nil, fmt.Errorf("selected container started during preflight")
		}
		removals = append(removals, removal{r, c, lock, owner})
	}
	removed := []string{}
	for _, item := range removals {
		if dryRun {
			removed = append(removed, item.lock.Name)
			continue
		}
		if item.container.State.Running {
			if err = e.Docker.Stop(ctx, item.container, item.owner); err != nil {
				return removed, err
			}
		}
		if err = e.Docker.Remove(ctx, item.container, item.owner); err != nil {
			return removed, err
		}
		if item.record.ID != "" {
			item.record.Action = "delete-container"
			item.record.Activity = time.Now().UTC()
			if err = item.lock.Save(item.record); err != nil {
				return removed, err
			}
		}
		removed = append(removed, item.lock.Name)
	}
	return removed, nil
}

// Missing metadata does not authorize adoption or startup. Deletion can still
// clean up a fully labelled owned container without modifying retained state.
func (e *Engine) orphanOwner(c docker.Container) (docker.Owner, error) {
	labels := c.Config.Labels
	owner := docker.Owner{Installation: e.Store.Installation, Session: labels[docker.Namespace+".session"], Workspace: labels[docker.Namespace+".workspace"], LocalName: labels[docker.Namespace+".local-name"]}
	id, err := hex.DecodeString(owner.Session)
	if err != nil || len(id) != 16 || hex.EncodeToString(id) != owner.Session {
		return owner, fmt.Errorf("invalid container session ownership")
	}
	if !filepath.IsAbs(owner.Workspace) || filepath.Clean(owner.Workspace) != owner.Workspace {
		return owner, fmt.Errorf("invalid container workspace ownership")
	}
	if err := environment.ValidateLocalName(owner.LocalName); err != nil {
		return owner, err
	}
	if strings.TrimPrefix(c.Name, "/") != environment.ContainerName(owner.Workspace, owner.LocalName) {
		return owner, fmt.Errorf("container name does not match its labelled identity")
	}
	return owner, c.Verify(owner)
}

func (e *Engine) RecreateAll(ctx context.Context, force bool, options Request) ([]string, error) {
	if options.Host == nil {
		options.Host = config.Snapshot()
	}
	names, err := e.selectContainers(ctx, &Selection{All: true, LocalName: options.LocalName})
	if err != nil {
		return nil, err
	}
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return nil, err
	}
	defer store.CloseAll(locks)
	type replacement struct {
		lock    *store.Locked
		record  store.Record
		spec    environment.Spec
		running bool
	}
	planned := []replacement{}
	for _, lock := range locks {
		r, err := lock.Load()
		if err != nil {
			return nil, err
		}
		if err = lock.RequireIdle(); err != nil {
			return nil, err
		}
		c, exists, err := e.inspect(ctx, r)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, os.ErrNotExist
		}
		request := options
		request.Workspace = r.Identity.Workspace
		request.LocalName = r.Identity.LocalName
		request.Recorded = &r.Identity
		request.Sources = r.Sources
		spec, err := e.Resolve(request)
		if err != nil {
			return nil, err
		}
		planned = append(planned, replacement{lock, r, spec, r.ManualStart || c.State.Running})
	}
	applied := []string{}
	for _, item := range planned {
		r, c, err := e.create(ctx, item.lock, item.spec, &item.record, force)
		if err != nil {
			return applied, err
		}
		if !item.running {
			if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
				return applied, err
			}
		}
		applied = append(applied, r.Identity.Name)
	}
	return applied, nil
}
