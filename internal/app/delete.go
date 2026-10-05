package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

type DeletePrompt struct {
	Containers            []string
	Sessions              []string
	IncompleteDirectories []string
}

type DeleteScope string

const (
	DeleteContainer DeleteScope = "container"
	DeleteSession   DeleteScope = "session"
)

type DeleteOptions struct {
	Selection Selection
	Scope     DeleteScope
	Orphaned  bool
	OlderThan time.Duration
	Force     bool
	DryRun    bool
	// Confirm asks about each destructive phase within Scope while locks remain
	// held. A nil callback with an explicit scope is non-interactive; dry runs
	// never confirm. An empty scope retains the interactive two-stage choice.
	Confirm func(DeletePrompt) (bool, error)
}

type DeleteResult struct {
	// Targets retains captured identities across a preview and a later form.
	Targets                       []SelectedTarget `json:"-"`
	Containers                    []string         `json:"containers"`
	Sessions                      []string         `json:"sessions"`
	Retained                      []string         `json:"retained_sessions"`
	DryRun                        bool             `json:"dry_run"`
	Cancelled                     bool             `json:"cancelled"`
	IncompleteDirectories         []string         `json:"incomplete_directories,omitempty"`
	RetainedIncompleteDirectories []string         `json:"retained_incomplete_directories,omitempty"`
}

func (r DeleteResult) DeletedCount() int {
	return len(r.Containers) + len(r.Sessions) + len(r.IncompleteDirectories)
}

func (e *Engine) deletionTargets(ctx context.Context, options DeleteOptions, cutoff time.Time) (selectedTargets, error) {
	selection := options.Selection
	filtered := selection.All || selection.Stopped || options.Orphaned || !cutoff.IsZero()
	if !filtered {
		return e.selectContainers(ctx, selection)
	}
	if len(selection.Targets) > 0 || len(selection.Captured) > 0 || selection.LocalName != "" {
		return nil, fmt.Errorf("use exact targets with optional --name, or selection filters, not both")
	}
	report, err := e.List(ctx, "")
	if err != nil {
		return nil, err
	}
	targets := map[string]SelectedTarget{}
	for _, view := range append(report.Sessions, report.UnmatchedContainers...) {
		if view.Uncommitted {
			continue
		}
		if selection.Stopped && (!view.Exists || view.Running) || options.Orphaned && view.Exists {
			continue
		}
		if !cutoff.IsZero() {
			if view.Error != "" || view.LastActivity.IsZero() {
				return nil, fmt.Errorf("cannot determine last activity for %s; inspect this environment before filtered deletion", view.Target)
			}
			if !view.LastActivity.Before(cutoff) {
				continue
			}
		}
		if view.Pending != nil {
			return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", view.Target, nil, view.Pending.RetryStep())
		}
		target := SelectedTarget{name: view.Target, sessionID: view.OwnerSessionID, record: recordAbsent, containerID: view.ContainerID}
		if view.SessionID != "" {
			r, err := e.Store.Find(ctx, view.SessionID, nil)
			if err != nil {
				return nil, err
			}
			target = selectedRecord(r)
		}
		targets[target.name] = target
	}
	return orderedTargets(targets), nil
}

// Recheck discovered state under the full lock set, including after a prompt.
// Do this before container deletion records its own new activity timestamp.
func (e *Engine) recheckDeleteFilters(ctx context.Context, locks []*store.Locked, options DeleteOptions, cutoff time.Time) error {
	if cutoff.IsZero() && !options.Orphaned {
		return nil
	}
	for _, lock := range locks {
		r, err := lock.Load()
		if err != nil {
			return err
		}
		if !cutoff.IsZero() && (r.Activity.IsZero() || !r.Activity.Before(cutoff)) {
			return fmt.Errorf("session activity changed since selection; preview cleanup again")
		}
		if options.Orphaned {
			_, exists, err := e.inspect(ctx, r)
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("container appeared since orphan selection; preview cleanup again")
			}
		}
	}
	return nil
}

// Keep the same endpoint locks across both choices. A concurrent open cannot
// recover a deleted container or replace the session between confirmations.
func (e *Engine) Delete(ctx context.Context, options DeleteOptions) (result DeleteResult, err error) {
	result = DeleteResult{Containers: []string{}, Sessions: []string{}, Retained: []string{}, DryRun: options.DryRun}
	if options.Scope != "" && options.Scope != DeleteContainer && options.Scope != DeleteSession {
		return result, fmt.Errorf("invalid deletion scope")
	}
	if options.Scope == "" && (options.Confirm == nil || options.DryRun) {
		return result, fmt.Errorf("choose container or session deletion scope")
	}
	if options.OlderThan < 0 {
		return result, fmt.Errorf("age must be positive")
	}
	var cutoff time.Time
	if options.OlderThan > 0 {
		cutoff = time.Now().Add(-options.OlderThan)
	}
	if len(options.Selection.Targets) > 0 && len(options.Selection.Captured) > 0 {
		return result, fmt.Errorf("use requested or captured targets, not both")
	}
	incomplete, remaining, err := e.incompleteDeletionTargets(ctx, options)
	if err != nil {
		return result, err
	}
	if len(incomplete) > 0 {
		// These directories have no ID-keyed lock. Creation and transfers hold
		// the namespace lock throughout preparation; keep it through prompts
		// and cleanup rather than inventing a session identity for their files.
		namespace, err := e.Store.LockNames(ctx)
		if err != nil {
			return result, err
		}
		defer fsutil.Unlock(namespace)
		for _, directory := range incomplete {
			if err := directory.Check(ctx); err != nil {
				return result, err
			}
			result.Targets = append(result.Targets, SelectedTarget{name: directory.Name, incomplete: directory})
			result.RetainedIncompleteDirectories = append(result.RetainedIncompleteDirectories, directory.Name)
		}
		options.Selection = remaining
	}
	defer func() {
		result.Retained = slices.DeleteFunc(result.Retained, func(name string) bool { return slices.Contains(result.Sessions, name) })
		result.RetainedIncompleteDirectories = slices.DeleteFunc(result.RetainedIncompleteDirectories, func(name string) bool { return slices.Contains(result.IncompleteDirectories, name) })
	}()
	var targets selectedTargets
	if len(incomplete) == 0 || len(remaining.Targets)+len(remaining.Captured) > 0 {
		targets, err = e.deletionTargets(ctx, options, cutoff)
		if err != nil {
			return result, err
		}
	}
	locks, err := targets.lock(ctx, e.Store)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	if err := e.recheckDeleteFilters(ctx, locks, options, cutoff); err != nil {
		return result, err
	}
	containers, err := e.deleteContainersLocked(ctx, targets, options.Selection, locks, options.Force, true)
	if err != nil {
		return result, err
	}
	sessionLocks := []*store.Locked{}
	for i, lock := range locks {
		r, err := lock.Load()
		if errors.Is(err, os.ErrNotExist) {
			if slices.Contains(containers, lock.Name) {
				result.Targets = append(result.Targets, targets[i])
			}
			continue
		}
		if err != nil {
			return result, err
		}
		if name := options.Selection.LocalName; name != "" && r.Settings.LocalName != name {
			return result, fmt.Errorf("local name does not match the selected session")
		}
		sessionLocks = append(sessionLocks, lock)
		result.Targets = append(result.Targets, selectedRecord(r))
		result.Retained = append(result.Retained, r.Directory)
	}
	include := options.Scope == DeleteSession
	if include {
		// Explicit whole-environment deletion preflights saved state before any
		// container removal. Force never bypasses durable-state idle checks.
		if _, err = e.planSessionDeletion(ctx, sessionLocks, true); err != nil {
			return result, err
		}
		if err = e.checkIncompleteDeletion(ctx, incomplete); err != nil {
			return result, err
		}
	}
	if options.Confirm != nil && !options.DryRun && len(containers) > 0 {
		ok, err := options.Confirm(DeletePrompt{Containers: containers})
		if err != nil {
			return result, err
		}
		if !ok {
			result.Cancelled = true
			return result, nil
		}
	}
	if err := e.recheckDeleteFilters(ctx, locks, options, cutoff); err != nil {
		return result, err
	}
	result.Containers, err = e.deleteContainersLocked(ctx, targets, options.Selection, locks, options.Force, options.DryRun)
	if err != nil {
		return result, err
	}
	if options.Confirm != nil && !options.DryRun && options.Scope != DeleteContainer && len(sessionLocks)+len(incomplete) > 0 {
		include, err = options.Confirm(DeletePrompt{Sessions: result.Retained, IncompleteDirectories: result.RetainedIncompleteDirectories})
		if err != nil {
			return result, err
		}
	}
	if !include {
		return result, nil
	}
	planned, err := e.planSessionDeletion(ctx, sessionLocks, options.DryRun)
	if err != nil {
		return result, err
	}
	if err = e.checkIncompleteDeletion(ctx, incomplete); err != nil {
		return result, err
	}
	result.Sessions, err = e.removeSessionState(ctx, planned, options.DryRun)
	if err != nil {
		return result, err
	}
	result.IncompleteDirectories, err = e.removeIncompleteDirectories(ctx, incomplete, options.DryRun)
	return result, err
}
