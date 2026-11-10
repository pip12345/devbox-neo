package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/store"
)

// Selection describes a request, not mutable discovery bookkeeping. Captured
// targets retain the identity chosen by an earlier preview instead of resolving
// a folder's potentially changed default again.
type Selection struct {
	Targets        []string
	ContainerNames []string
	IncompleteDirs []string
	Captured       []SelectedTarget
	LocalName      string
	All            bool
	Stopped        bool
}

type recordExpectation uint8

const (
	recordUnchecked recordExpectation = iota
	recordPresent
)

// SelectedTarget keeps one resource's lock ownership, record expectation and
// optional exact runtime together. An absent record still has an owner ID: losing
// session.json must not bypass the same operation lock or attachment leases.
// Failed selections carry an error, not mutation authority. Fields stay private
// so frontends can retain selections, not manufacture them.
type SelectedTarget struct {
	name        string
	sessionID   string
	record      recordExpectation
	containerID string
	incomplete  *store.IncompleteDirectory
	unmatched   bool
	err         error
}

func (t SelectedTarget) Name() string { return t.name }
func (t SelectedTarget) selector() string {
	if t.record == recordPresent {
		return t.sessionID
	}
	return t.name
}
func selectedRecord(r store.Record) SelectedTarget {
	return SelectedTarget{name: r.PublicName(), sessionID: r.ID, record: recordPresent}
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
func (e *Engine) selectSavedSessions(ctx context.Context) (selectedTargets, error) {
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	targets := map[string]SelectedTarget{}
	for _, entry := range entries {
		if entry.Pending == nil && os.IsNotExist(entry.Err) {
			continue
		}
		target := SelectedTarget{name: entry.Name, err: entry.Err}
		if entry.Err == nil {
			target = selectedRecord(entry.Record)
		}
		if entry.Pending != nil {
			target.err = commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", target.name, nil, entry.Pending.RetryStep())
		}
		targets[entry.Name] = target
	}
	return orderedTargets(targets), nil
}

func (e *Engine) lockSelectedSession(ctx context.Context, target SelectedTarget) (*store.Locked, store.Record, error) {
	lock, err := e.Store.Lock(ctx, target.sessionID)
	if err != nil {
		return nil, store.Record{}, err
	}
	r, err := lock.Load()
	if err != nil {
		lock.Close()
		return nil, r, err
	}
	return lock, r, nil
}

func (e *Engine) selectContainers(ctx context.Context, selection Selection) (selectedTargets, error) {
	exact := len(selection.Targets) > 0 || len(selection.Captured) > 0 || len(selection.ContainerNames) > 0
	if len(selection.Targets) > 0 && len(selection.Captured) > 0 {
		return nil, fmt.Errorf("use requested or captured targets, not both")
	}
	if selection.LocalName != "" && (len(selection.Captured) > 0 || len(selection.ContainerNames) > 0) {
		return nil, fmt.Errorf("--name requires an explicit folder target")
	}
	targets := map[string]SelectedTarget{}
	add := func(name string, target *SelectedTarget, err error) {
		if err != nil {
			targets[name] = SelectedTarget{name: name, err: err}
		} else if target != nil {
			targets[target.name] = *target
		}
	}
	if !exact {
		return nil, fmt.Errorf("provide a target or an explicit selection flag")
	}
	for _, input := range selection.Targets {
		target, err := e.selectContainer(ctx, input, selection.LocalName)
		add(input, target, err)
	}
	for _, name := range selection.ContainerNames {
		target, err := e.selectUnmatchedContainer(ctx, name)
		add(name, target, err)
	}
	for _, captured := range selection.Captured {
		if captured.err != nil {
			targets[captured.name] = captured
			continue
		}
		var target *SelectedTarget
		var err error
		if captured.unmatched {
			c, exists, inspectErr := e.Docker.InspectID(ctx, captured.containerID)
			if inspectErr != nil {
				add(captured.name, nil, inspectErr)
				continue
			}
			if exists {
				target, err = e.selectUnmatchedInstance(ctx, c)
			} else {
				_, occupied, inspectErr := e.Docker.Inspect(ctx, captured.name)
				if inspectErr != nil {
					add(captured.name, nil, inspectErr)
					continue
				}
				if occupied {
					add(captured.name, nil, fmt.Errorf("selected container instance changed; preview deletion again"))
					continue
				}
			}
		} else {
			target, err = e.selectContainer(ctx, captured.selector(), "")
		}
		if err != nil {
			add(captured.name, nil, err)
			continue
		}
		if target == nil {
			continue
		}
		if target.sessionID != captured.sessionID || (captured.containerID != "" && target.containerID != captured.containerID) {
			add(captured.name, nil, fmt.Errorf("selected target changed; preview deletion again"))
			continue
		}
		targets[target.name] = *target
	}
	return orderedTargets(targets), nil
}

func (e *Engine) selectContainer(ctx context.Context, target, localName string) (*SelectedTarget, error) {
	r, err := e.Locate(ctx, target, localName)
	if err != nil {
		return nil, err
	}
	selected := selectedRecord(r)
	return &selected, nil
}

func (e *Engine) selectUnmatchedContainer(ctx context.Context, name string) (*SelectedTarget, error) {
	c, exists, err := e.Docker.Inspect(ctx, name)
	if err != nil || !exists {
		return nil, err
	}
	return e.selectUnmatchedInstance(ctx, c)
}

func (e *Engine) selectUnmatchedInstance(ctx context.Context, c docker.Container) (*SelectedTarget, error) {
	owner, err := e.orphanOwner(c)
	if err != nil {
		return nil, err
	}
	pending, err := e.Store.PendingID(owner.Session)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", pending.Source, nil, pending.RetryStep())
	}
	r, findErr := e.Store.Find(ctx, owner.Session, nil)
	if findErr == nil && r.Applied.ContainerID == c.ID {
		return nil, fmt.Errorf("container is the recorded runtime; select public session %s instead", r.PublicName())
	}
	if findErr != nil && !sessionAbsent(findErr) && !incompleteInventory(findErr) {
		return nil, findErr
	}
	return &SelectedTarget{name: strings.TrimPrefix(c.Name, "/"), sessionID: owner.Session, containerID: c.ID, unmatched: true}, nil
}
