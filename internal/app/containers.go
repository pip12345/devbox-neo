package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
	Target              string                    `json:"-"`
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
}

func recordView(r store.Record) View {
	return View{Target: r.ID, ContainerName: r.Applied.Creation.Name, Workspace: r.Settings.Workspace, LocalName: r.Settings.LocalName, Sources: r.Settings.Sources, ManualStart: r.Settings.ManualStart, Harness: r.Applied.Definition.Name, SessionID: r.ID, LastActivity: r.Activity, LastAction: r.Action, CreatedAt: r.Created}
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
		if entry.Record.Applied.SetupContainer != "" {
			byContainer[entry.Record.Applied.SetupContainer] = entry.Name
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
		}
		view.ContainerID, view.ContainerName = container.ID, name
		view.Exists, view.Running = true, container.State.Running
		if found && entry.Err == nil && entry.Record.ID != "" {
			if err := container.Verify(e.owner(entry.Record)); err != nil {
				view.Error = err.Error()
			} else if container.ID != entry.Record.Applied.SetupContainer || container.Image != entry.Record.Applied.ImageID {
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
	desired, err := e.Resolve(Request{Workspace: r.Settings.Workspace, LocalName: r.Settings.LocalName, SessionID: r.ID, Sources: r.Settings.Sources})
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
		return commanderror.New("container_missing", "Container not found; its logs are unavailable.", r.ID, nil,
			commanderror.Next("Inspect session", "status", r.ID))
	}
	return e.Docker.Logs(ctx, c, e.owner(r), follow, tail, e.Streams.Out, e.Streams.Err)
}

type Selection struct {
	Targets      []string
	LocalName    string
	All          bool
	Stopped      bool
	selectedIDs  map[string]string
	containerIDs map[string]string
	operationIDs map[string]string
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
	if selection.operationIDs == nil {
		selection.operationIDs = map[string]string{}
	}
	if selection.containerIDs == nil {
		selection.containerIDs = map[string]string{}
	}
	names := map[string]bool{}
	if selection.All || selection.Stopped {
		containers, err := e.Docker.Inventory(ctx, e.Store.Installation)
		if err != nil {
			return nil, err
		}
		entries, err := e.Store.Inventory(ctx)
		if err != nil {
			return nil, err
		}
		byContainer := map[string]store.Record{}
		for _, entry := range entries {
			if entry.Record.Applied.SetupContainer != "" {
				byContainer[entry.Record.Applied.SetupContainer] = entry.Record
			}
		}
		for _, c := range containers {
			if selection.Stopped && c.State.Running {
				continue
			}
			key := strings.TrimPrefix(c.Name, "/")
			if r, ok := byContainer[c.ID]; ok {
				key = r.Directory
				selection.selectedIDs[key] = r.ID
				selection.operationIDs[key] = r.ID
			}
			if selection.operationIDs[key] == "" {
				owner, err := e.orphanOwner(c)
				if err != nil {
					return nil, err
				}
				selection.operationIDs[key] = owner.Session
			}
			names[key] = true
		}
	} else {
		if len(selection.Targets) == 0 {
			return nil, fmt.Errorf("provide a target or an explicit selection flag")
		}
		for _, target := range selection.Targets {
			r, err := e.Locate(ctx, target, selection.LocalName)
			if err != nil {
				byID := environment.IsSessionTarget(target) && (sessionAbsent(err) || incompleteInventory(err))
				if byID {
					live, inventoryErr := e.Docker.Inventory(ctx, e.Store.Installation)
					if inventoryErr != nil {
						return nil, inventoryErr
					}
					match := ""
					for _, c := range live {
						if c.Config.Labels[docker.Namespace+".session"] == target {
							if match != "" {
								return nil, fmt.Errorf("multiple containers use this session ID; select an exact Docker name for cleanup")
							}
							match = strings.TrimPrefix(c.Name, "/")
						}
					}
					if match == "" {
						return nil, err
					}
					target = match
				}
				if byID || (errors.Is(err, os.ErrNotExist) && strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\")) {
					c, exists, inspectErr := e.Docker.Inspect(ctx, target)
					if inspectErr != nil {
						return nil, inspectErr
					}
					if exists {
						owner, ownerErr := e.orphanOwner(c)
						if ownerErr != nil {
							return nil, ownerErr
						}
						pending, pendingErr := e.Store.PendingID(owner.Session)
						if pendingErr != nil {
							return nil, pendingErr
						}
						if pending != nil {
							return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", owner.Session, nil, pending.RetryStep())
						}
						linked, findErr := e.Store.Find(ctx, owner.Session, nil)
						if findErr == nil {
							if linked.Applied.SetupContainer != c.ID {
								return nil, fmt.Errorf("container is not the recorded session instance")
							}
							names[linked.Directory] = true
							selection.selectedIDs[linked.Directory] = linked.ID
							selection.operationIDs[linked.Directory] = linked.ID
							selection.containerIDs[linked.Directory] = c.ID
							continue
						}
						if !sessionAbsent(findErr) && !incompleteInventory(findErr) {
							return nil, findErr
						}
						selection.operationIDs[target] = owner.Session
					}
					if !exists {
						continue
					}
					names[target] = true
					selection.selectedIDs[target] = ""
					if exists {
						selection.containerIDs[target] = c.ID
					}
					continue
				}
				return nil, err
			}
			names[r.Directory] = true
			selection.selectedIDs[r.Directory] = r.ID
			selection.operationIDs[r.Directory] = r.ID
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
	locks, err := e.Store.LockAll(ctx, names, selection.operationIDs)
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
				if err == nil {
					pending, pendingErr := e.Store.PendingID(owner.Session)
					if pendingErr != nil {
						return nil, pendingErr
					}
					if pending != nil {
						return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", owner.Session, nil, pending.RetryStep())
					}
					// A retained session requires its own operation lock and leases,
					// even when the user selected the Docker name rather than its ID.
					_, findErr := e.Store.Find(ctx, owner.Session, nil)
					if findErr == nil {
						return nil, fmt.Errorf("container has a saved session; refresh selection and delete by session ID")
					}
					if !sessionAbsent(findErr) && !incompleteInventory(findErr) {
						return nil, findErr
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if expected := selection.containerIDs[lock.Name]; expected != "" && c.ID != expected {
			return nil, fmt.Errorf("selected container instance changed; retry deletion")
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
			removed = append(removed, strings.TrimPrefix(item.container.Name, "/"))
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
		// Docker removal has committed even if recording activity fails next.
		removed = append(removed, strings.TrimPrefix(item.container.Name, "/"))
		if item.record.ID != "" {
			item.record.Action = "delete-container"
			item.record.Activity = time.Now().UTC()
			if err = item.lock.Save(item.record); err != nil {
				return removed, err
			}
		}
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
	return owner, c.Verify(owner)
}

func (e *Engine) RecreateAll(ctx context.Context, force bool, options Request) ([]string, error) {
	if options.Host == nil {
		options.Host = config.Snapshot()
	}
	selection := Selection{All: true, LocalName: options.LocalName}
	names, err := e.selectContainers(ctx, &selection)
	if err != nil {
		return nil, err
	}
	locks, err := e.Store.LockAll(ctx, names, selection.operationIDs)
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
		request.Workspace = r.Settings.Workspace
		request.LocalName = r.Settings.LocalName
		request.SessionID = r.ID
		request.Sources = r.Settings.Sources
		spec, err := e.Resolve(request)
		if err != nil {
			return nil, err
		}
		planned = append(planned, replacement{lock, r, spec, r.Settings.ManualStart || c.State.Running})
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
		applied = append(applied, r.ID)
	}
	return applied, nil
}
