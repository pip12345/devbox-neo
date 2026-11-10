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
	Target              string                    `json:"target"`
	Workspace           string                    `json:"workspace,omitempty"`
	LocalName           string                    `json:"local_name,omitempty"`
	Default             bool                      `json:"default"`
	Sources             []config.Reference        `json:"sources,omitempty"`
	ManualStart         bool                      `json:"manual_start"`
	Harness             string                    `json:"harness,omitempty"`
	SessionID           string                    `json:"session_id,omitempty"`
	StoragePath         string                    `json:"storage_path,omitempty"`
	LastActivity        time.Time                 `json:"last_activity,omitempty"`
	LastAction          string                    `json:"last_action,omitempty"`
	CreatedAt           time.Time                 `json:"created_at,omitempty"`
	ContainerID         string                    `json:"container_id,omitempty"`
	ContainerName       string                    `json:"container_name,omitempty"`
	OwnerSessionID      string                    `json:"-"`
	Uncommitted         bool                      `json:"-"`
	Exists              bool                      `json:"exists"`
	Running             bool                      `json:"running"`
	ImageMissing        bool                      `json:"image_missing"`
	Error               string                    `json:"error,omitempty"`
	Desired             environment.Change        `json:"desired_change,omitempty"`
	PendingInputChanges []environment.InputChange `json:"pending_input_changes,omitempty"`
	ConfigError         string                    `json:"config_error,omitempty"`
	Pending             *store.Reservation        `json:"pending_transfer,omitempty"`
	Warnings            []string                  `json:"-"`
}

func recordView(r store.Record) View {
	return View{Target: r.PublicName(), ContainerName: r.Applied.Creation.Name, Workspace: r.Settings.Workspace, LocalName: r.Settings.LocalName, Sources: r.Settings.Sources, ManualStart: r.Settings.ManualStart, Harness: r.Applied.Definition.Name, SessionID: r.ID, LastActivity: r.Activity, LastAction: r.Action, CreatedAt: r.Created}
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
	byContainer := map[string]string{}
	for _, entry := range entries {
		records[entry.Name] = entry
		if entry.Record.Applied.ContainerID != "" {
			byContainer[entry.Record.Applied.ContainerID] = entry.Name
		}
	}
	result := []View{}
	for _, container := range live {
		name := strings.TrimPrefix(container.Name, "/")
		directory := byContainer[container.ID]
		entry, found := records[directory]
		if sessions && !found {
			continue
		}
		view := View{Target: name}
		if found && entry.Record.ID != "" {
			view = recordView(entry.Record)
			view.Target = entry.Record.PublicName()
		}
		if view.SessionID != "" {
			view.StoragePath = filepath.Join(e.Store.Home, "sessions", view.SessionID)
		}
		view.ContainerID, view.ContainerName = container.ID, name
		view.Exists, view.Running = true, container.State.Running
		if found && entry.Err == nil && entry.Record.ID != "" {
			if err := container.Verify(e.owner(entry.Record)); err != nil {
				view.Error = err.Error()
			} else if container.ID != entry.Record.Applied.ContainerID || container.Image != entry.Record.Applied.ImageID {
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
		delete(records, directory)
	}
	if sessions {
		for name, entry := range records {
			view := recordView(entry.Record)
			if view.Target == "" {
				view.Target = name
			}
			if view.SessionID != "" {
				view.StoragePath = filepath.Join(e.Store.Home, "sessions", view.SessionID)
			}
			view.Pending = entry.Pending
			if entry.Err != nil {
				view.Error = entry.Err.Error()
				view.Uncommitted = errors.Is(entry.Err, os.ErrNotExist) && entry.Pending == nil
			}
			result = append(result, view)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Target < result[j].Target })
	return result
}
func (e *Engine) desiredStatus(view *View, r store.Record) {
	desired, err := e.Resolve(ResolveRequest{Workspace: r.Settings.Workspace, LocalName: r.Settings.LocalName, RepairTarget: r.PublicName(), Sources: r.Settings.Sources})
	view.Warnings = desired.Warnings
	if err != nil {
		view.ConfigError = err.Error()
	} else {
		report := environment.CompareInputs(r.Applied.Inputs, desired.Inputs)
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
		return commanderror.New("container_missing", "Container not found; its logs are unavailable.", r.PublicName(), nil, commanderror.Next("Inspect session", "status", r.PublicName()))
	}
	return e.Docker.Logs(ctx, c, e.owner(r), follow, tail, e.Streams.Out, e.Streams.Err)
}
func (e *Engine) DeleteContainers(ctx context.Context, selection Selection, force bool) ([]string, error) {
	result, err := e.Delete(ctx, DeleteOptions{Selection: selection, Scope: DeleteContainer, Force: force})
	return result.Containers, err
}
func (e *Engine) deleteContainerLocked(ctx context.Context, target SelectedTarget, selection Selection, lock *store.Locked, force, dryRun bool) (string, error) {
	r, loadErr := lock.Load()
	if loadErr != nil && (!target.unmatched || !os.IsNotExist(loadErr)) {
		return "", loadErr
	}
	if target.record == recordPresent && r.ID != target.sessionID {
		return "", fmt.Errorf("selected session changed; retry deletion")
	}
	if !force {
		if err := lock.RequireIdle(); err != nil {
			return "", err
		}
	}
	var c docker.Container
	var exists bool
	var owner docker.Owner
	var err error
	if target.unmatched {
		c, exists, err = e.Docker.InspectID(ctx, target.containerID)
		if err == nil && exists {
			owner, err = e.orphanOwner(c)
			if err == nil && owner.Session != target.sessionID {
				err = fmt.Errorf("selected container ownership changed")
			}
			if loadErr == nil && r.Applied.ContainerID == c.ID {
				return "", fmt.Errorf("container became the recorded runtime; select public session %s", r.PublicName())
			}
		}
	} else {
		c, exists, err = e.inspect(ctx, r)
		owner = e.owner(r)
	}
	if err != nil || !exists {
		return "", err
	}
	if target.containerID != "" && c.ID != target.containerID {
		return "", fmt.Errorf("selected container instance changed; retry deletion")
	}
	if selection.LocalName != "" && owner.LocalName != selection.LocalName {
		return "", fmt.Errorf("local name does not match the selected container")
	}
	if selection.Stopped && c.State.Running {
		return "", fmt.Errorf("selected container started during preflight")
	}
	name := strings.TrimPrefix(c.Name, "/")
	if dryRun {
		return name, nil
	}
	if c.State.Running {
		if err := e.Docker.Stop(ctx, c, owner); err != nil {
			return "", err
		}
	}
	if err := e.Docker.Remove(ctx, c, owner); err != nil {
		return "", err
	}
	// Docker removal has committed even if recording activity fails next.
	// Unmatched-resource cleanup never changes a saved session's activity.
	if !target.unmatched && r.ID != "" {
		r.Action, r.Activity = "delete-container", time.Now().UTC()
		if err := lock.Save(r); err != nil {
			return name, err
		}
	}
	return name, nil
}

// Missing metadata does not authorize adoption or startup. Deletion can still
// clean up a fully labelled owned container without modifying retained state.
func (e *Engine) orphanOwner(c docker.Container) (docker.Owner, error) {
	labels := c.Config.Labels
	owner := docker.Owner{Installation: e.Store.Installation, Session: labels[docker.Namespace+".session"], Workspace: labels[docker.Namespace+".workspace"], LocalName: labels[docker.Namespace+".local-name"]}
	id, err := hex.DecodeString(owner.Session)
	if err != nil || len(id) != 6 || hex.EncodeToString(id) != owner.Session {
		return owner, fmt.Errorf("invalid container session ownership")
	}
	return owner, c.Verify(owner)
}
func (e *Engine) RecreateAll(ctx context.Context, image bool, options RecreateOptions) (BatchReport[Result], error) {
	if options.Host == nil {
		options.Host = config.Snapshot()
	}
	targets, err := e.selectSavedSessions(ctx)
	if err != nil {
		return BatchReport[Result]{}, err
	}
	b := newBatch[Result](targets)
	b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[Result]) error {
		lock, r, err := e.lockSelectedSession(ctx, target)
		if err != nil {
			return err
		}
		defer lock.Close()
		item.Session = r.PublicName()
		plan, err := e.planRecreate(ctx, lock, r, options, image)
		e.reportWarnings(plan.spec.Warnings)
		if err != nil {
			return err
		}
		result, err := e.applyRecreate(ctx, plan)
		item.Result = &result
		return err
	})
	return b.report, b.err(ctx)
}
