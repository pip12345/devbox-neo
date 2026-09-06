package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

type View struct {
	Name         string             `json:"name"`
	Workspace    string             `json:"workspace,omitempty"`
	Profile      string             `json:"profile,omitempty"`
	Harness      string             `json:"harness,omitempty"`
	SessionID    string             `json:"session_id,omitempty"`
	LastActivity time.Time          `json:"last_activity,omitempty"`
	ContainerID  string             `json:"container_id,omitempty"`
	Exists       bool               `json:"exists"`
	Running      bool               `json:"running"`
	Error        string             `json:"error,omitempty"`
	Desired      environment.Change `json:"desired_change,omitempty"`
	ConfigError  string             `json:"config_error,omitempty"`
}

func recordView(r store.Record) View {
	return View{Name: r.Identity.Name, Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, Harness: r.Definition.Name, SessionID: r.ID, LastActivity: r.Activity}
}
func (e *Engine) List(ctx context.Context, sessions bool) ([]View, error) {
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	live, err := e.Docker.Inventory(ctx, e.Store.Installation)
	if err != nil {
		return nil, err
	}
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
		if found && entry.Err == nil {
			view = recordView(entry.Record)
			view.ContainerID = container.ID
			view.Exists = true
			view.Running = container.State.Running
			if err := container.Verify(e.owner(entry.Record)); err != nil {
				view.Error = err.Error()
			} else if container.ID != entry.Record.SetupContainer || container.Image != entry.Record.ImageID {
				view.Error = "container differs from committed creation contract"
			}
		} else if found {
			view.Error = entry.Err.Error()
		} else {
			view.Error = "container has no durable record"
		}
		result = append(result, view)
		delete(records, name)
	}
	if sessions {
		for name, entry := range records {
			view := recordView(entry.Record)
			view.Name = name
			if entry.Err != nil {
				view.Error = entry.Err.Error()
			}
			result = append(result, view)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
func (e *Engine) Status(ctx context.Context, target, profile string) (View, error) {
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return View{}, err
	}
	view := recordView(r)
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return view, err
	}
	view.Exists = exists
	view.Running = exists && c.State.Running
	if exists {
		view.ContainerID = c.ID
	}
	desired, resolveErr := e.Resolve(Request{Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, ExpectedName: r.Identity.Name})
	if resolveErr != nil {
		view.ConfigError = resolveErr.Error()
	} else {
		view.Desired = environment.Compare(r.Applied, desired.FingerprintsFor(r.ImageID))
	}
	return view, nil
}
func (e *Engine) Logs(ctx context.Context, target, profile string, follow bool, tail string) error {
	r, err := e.Locate(ctx, target, profile)
	if err != nil {
		return err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("container is missing")
	}
	return e.Docker.Logs(ctx, c, e.owner(r), follow, tail, e.Streams.Out, e.Streams.Err)
}

type Selection struct {
	Targets []string
	Profile string
	All     bool
	Stopped bool
}

func (e *Engine) selectContainers(ctx context.Context, selection Selection) ([]string, error) {
	if (selection.All && selection.Stopped) || ((selection.All || selection.Stopped) && len(selection.Targets) > 0) {
		return nil, fmt.Errorf("use exact targets, --all, or --stopped, not a combination")
	}
	names := map[string]bool{}
	if selection.All || selection.Stopped {
		containers, err := e.Docker.Inventory(ctx, e.Store.Installation)
		if err != nil {
			return nil, err
		}
		for _, c := range containers {
			if selection.Profile != "" && c.Config.Labels[docker.Namespace+".slot"] != "profile:"+selection.Profile {
				continue
			}
			if !selection.Stopped || !c.State.Running {
				names[strings.TrimPrefix(c.Name, "/")] = true
			}
		}
	} else {
		if len(selection.Targets) == 0 {
			return nil, fmt.Errorf("provide a target or an explicit selection flag")
		}
		for _, target := range selection.Targets {
			r, err := e.Locate(ctx, target, selection.Profile)
			if err != nil {
				if os.IsNotExist(err) && strings.HasPrefix(target, docker.Namespace+"-") && !strings.ContainsAny(target, "/\\") {
					names[target] = true
					continue
				}
				return nil, err
			}
			names[r.Identity.Name] = true
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
	names, err := e.selectContainers(ctx, selection)
	if err != nil {
		return nil, err
	}
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return nil, err
	}
	defer store.CloseAll(locks)
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
		if selection.Profile != "" && owner.Slot != "profile:"+selection.Profile {
			return nil, fmt.Errorf("profile does not match the selected container")
		}
		if selection.Stopped && c.State.Running {
			return nil, fmt.Errorf("selected container started during preflight")
		}
		removals = append(removals, removal{r, c, lock, owner})
	}
	removed := []string{}
	for _, item := range removals {
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
	owner := docker.Owner{Installation: e.Store.Installation, Session: labels[docker.Namespace+".session"], Workspace: labels[docker.Namespace+".workspace"], Slot: labels[docker.Namespace+".slot"]}
	id, err := hex.DecodeString(owner.Session)
	if err != nil || len(id) != 16 || hex.EncodeToString(id) != owner.Session {
		return owner, fmt.Errorf("invalid container session ownership")
	}
	if !filepath.IsAbs(owner.Workspace) || filepath.Clean(owner.Workspace) != owner.Workspace {
		return owner, fmt.Errorf("invalid container workspace ownership")
	}
	if owner.Slot != "project" {
		profile, ok := strings.CutPrefix(owner.Slot, "profile:")
		if !ok || !config.Name.MatchString(profile) {
			return owner, fmt.Errorf("invalid container slot ownership")
		}
	}
	if strings.TrimPrefix(c.Name, "/") != environment.ContainerName(owner.Workspace, owner.Slot) {
		return owner, fmt.Errorf("container name does not match its labelled identity")
	}
	return owner, c.Verify(owner)
}

func (e *Engine) RecreateAll(ctx context.Context, force bool, options Request) ([]string, error) {
	if options.Host == nil {
		options.Host = config.Snapshot()
	}
	names, err := e.selectContainers(ctx, Selection{All: true, Profile: options.Profile})
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
		request.Profile = r.Identity.Profile
		request.ExpectedName = r.Identity.Name
		spec, err := e.Resolve(request)
		if err != nil {
			return nil, err
		}
		planned = append(planned, replacement{lock, r, spec, c.State.Running})
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
