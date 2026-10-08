package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// Selection describes a request, not mutable discovery bookkeeping. Captured
// targets retain the identity chosen by an earlier preview instead of resolving
// a folder's potentially changed default again.
type Selection struct {
	Targets   []string
	Captured  []SelectedTarget
	LocalName string
	All       bool
	Stopped   bool
}

type recordExpectation uint8

const (
	recordUnchecked recordExpectation = iota
	recordPresent
	recordAbsent
)

// SelectedTarget keeps one resource's lock ownership, record expectation and
// optional exact runtime together. An absent record still has an owner ID: losing
// session.json must not bypass the same operation lock or attachment leases.
// Fields stay private so frontends can retain selections, not manufacture them.
type SelectedTarget struct {
	name        string
	sessionID   string
	record      recordExpectation
	containerID string
	incomplete  *store.IncompleteDirectory
}

func (t SelectedTarget) Name() string { return t.name }
func (t SelectedTarget) selector() string {
	if t.record == recordPresent {
		return t.sessionID
	}
	return t.name
}
func selectedRecord(r store.Record) SelectedTarget {
	return SelectedTarget{name: r.Directory, sessionID: r.ID, record: recordPresent}
}

type selectedTargets []SelectedTarget

func orderedTargets(targets map[string]SelectedTarget) selectedTargets {
	result := make(selectedTargets, 0, len(targets))
	for _, target := range targets {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result
}
func (targets selectedTargets) lock(ctx context.Context, state *store.Store) ([]*store.Locked, error) {
	names := make([]string, 0, len(targets))
	ids := make(map[string]string, len(targets))
	for _, target := range targets {
		names = append(names, target.name)
		ids[target.name] = target.sessionID
	}
	return state.LockAll(ctx, names, ids)
}

func (e *Engine) selectSavedSessions(ctx context.Context) (selectedTargets, error) {
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	targets := map[string]SelectedTarget{}
	ids := map[string]bool{}
	for _, entry := range entries {
		if entry.Pending != nil {
			return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", entry.Name, nil, entry.Pending.RetryStep())
		}
		// Incomplete allocations have no saved session to apply. Their files
		// belong to explicit cleanup, not bulk recreation.
		if os.IsNotExist(entry.Err) {
			continue
		}
		if entry.Err != nil {
			return nil, fmt.Errorf("cannot select saved session %s: %w", entry.Name, entry.Err)
		}
		if ids[entry.Record.ID] {
			return nil, fmt.Errorf("ambiguous session identity; inspect session inventory")
		}
		ids[entry.Record.ID] = true
		targets[entry.Name] = selectedRecord(entry.Record)
	}
	return orderedTargets(targets), nil
}

func (e *Engine) selectContainers(ctx context.Context, selection Selection) (selectedTargets, error) {
	exact := len(selection.Targets) > 0 || len(selection.Captured) > 0
	if (selection.All && selection.Stopped) || ((selection.All || selection.Stopped) && exact) || (len(selection.Targets) > 0 && len(selection.Captured) > 0) {
		return nil, fmt.Errorf("use exact targets, --all, or --stopped, not a combination")
	}
	if selection.LocalName != "" && (selection.All || selection.Stopped || len(selection.Captured) > 0) {
		return nil, fmt.Errorf("--name requires an explicit folder target")
	}
	targets := map[string]SelectedTarget{}
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
			target := SelectedTarget{name: strings.TrimPrefix(c.Name, "/")}
			if r, ok := byContainer[c.ID]; ok {
				target = selectedRecord(r)
			}
			if target.sessionID == "" {
				owner, err := e.orphanOwner(c)
				if err != nil {
					return nil, err
				}
				target.sessionID = owner.Session
			}
			targets[target.name] = target
		}
	} else {
		if !exact {
			return nil, fmt.Errorf("provide a target or an explicit selection flag")
		}
		for _, input := range selection.Targets {
			target, err := e.selectContainer(ctx, input, selection.LocalName)
			if err != nil {
				return nil, err
			}
			if target != nil {
				targets[target.name] = *target
			}
		}
		for _, captured := range selection.Captured {
			target, err := e.selectContainer(ctx, captured.selector(), "")
			if err != nil {
				return nil, err
			}
			if target == nil {
				continue
			}
			if target.sessionID != captured.sessionID || (captured.containerID != "" && target.containerID != captured.containerID) {
				return nil, fmt.Errorf("selected target changed; preview deletion again")
			}
			targets[target.name] = *target
		}
	}
	return orderedTargets(targets), nil
}

func (e *Engine) selectContainer(ctx context.Context, target, localName string) (*SelectedTarget, error) {
	r, err := e.Locate(ctx, target, localName)
	if err == nil {
		selected := selectedRecord(r)
		return &selected, nil
	}
	byID := environment.IsSessionID(target) && (sessionAbsent(err) || incompleteInventory(err))
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
	if !byID && !(errors.Is(err, os.ErrNotExist) && strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\")) {
		return nil, err
	}
	c, exists, err := e.Docker.Inspect(ctx, target)
	if err != nil || !exists {
		return nil, err
	}
	owner, err := e.orphanOwner(c)
	if err != nil {
		return nil, err
	}
	pending, err := e.Store.PendingID(owner.Session)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", owner.Session, nil, pending.RetryStep())
	}
	linked, findErr := e.Store.Find(ctx, owner.Session, nil)
	if findErr == nil {
		if linked.Applied.SetupContainer != c.ID {
			return nil, fmt.Errorf("container is not the recorded session instance")
		}
		selected := selectedRecord(linked)
		selected.containerID = c.ID
		return &selected, nil
	}
	if !sessionAbsent(findErr) && !incompleteInventory(findErr) {
		return nil, findErr
	}
	return &SelectedTarget{name: target, sessionID: owner.Session, record: recordAbsent, containerID: c.ID}, nil
}
