package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"time"

	"devbox/internal/store"
)

type DeletePrompt struct {
	Containers []string
	Sessions   []string
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
	Containers []string `json:"containers"`
	Sessions   []string `json:"sessions"`
	Retained   []string `json:"retained_sessions"`
	DryRun     bool     `json:"dry_run"`
	Cancelled  bool     `json:"cancelled"`
}

func (e *Engine) deletionTargets(ctx context.Context, options DeleteOptions, cutoff time.Time) ([]string, error) {
	selection := options.Selection
	filtered := selection.All || selection.Stopped || options.Orphaned || !cutoff.IsZero()
	if !filtered {
		return e.selectContainers(ctx, &selection)
	}
	if len(selection.Targets) > 0 || selection.LocalName != "" {
		return nil, fmt.Errorf("use exact targets with optional --name, or selection filters, not both")
	}
	report, err := e.List(ctx, "")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, view := range append(report.Sessions, report.UnmatchedContainers...) {
		if selection.Stopped && (!view.Exists || view.Running) || options.Orphaned && view.Exists {
			continue
		}
		if !cutoff.IsZero() {
			if view.Error != "" || view.LastActivity.IsZero() {
				return nil, fmt.Errorf("cannot determine last activity for %s; inspect this environment before filtered deletion", view.Name)
			}
			if !view.LastActivity.Before(cutoff) {
				continue
			}
		}
		names = append(names, view.Name)
	}
	sort.Strings(names)
	return names, nil
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
func (e *Engine) Delete(ctx context.Context, options DeleteOptions) (DeleteResult, error) {
	result := DeleteResult{Containers: []string{}, Sessions: []string{}, Retained: []string{}, DryRun: options.DryRun}
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
	options.Selection.selectedIDs = map[string]string{}
	names, err := e.deletionTargets(ctx, options, cutoff)
	if err != nil {
		return result, err
	}
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	if err := e.recheckDeleteFilters(ctx, locks, options, cutoff); err != nil {
		return result, err
	}
	containers, err := e.deleteContainersLocked(ctx, options.Selection, locks, options.Force, true)
	if err != nil {
		return result, err
	}
	sessionLocks := []*store.Locked{}
	for _, lock := range locks {
		r, err := lock.Load()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		if name := options.Selection.LocalName; name != "" && r.Identity.LocalName != name {
			return result, fmt.Errorf("local name does not match the selected session")
		}
		sessionLocks = append(sessionLocks, lock)
		result.Retained = append(result.Retained, lock.Name)
	}
	include := options.Scope == DeleteSession
	if include {
		// Explicit whole-environment deletion preflights saved state before any
		// container removal. Force never bypasses durable-state idle checks.
		if _, err = e.planSessionDeletion(ctx, sessionLocks, true); err != nil {
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
	result.Containers, err = e.deleteContainersLocked(ctx, options.Selection, locks, options.Force, options.DryRun)
	if err != nil {
		return result, err
	}
	if options.Confirm != nil && !options.DryRun && options.Scope != DeleteContainer && len(sessionLocks) > 0 {
		include, err = options.Confirm(DeletePrompt{Sessions: result.Retained})
		if err != nil {
			return result, err
		}
	}
	if !include || len(sessionLocks) == 0 {
		return result, nil
	}
	planned, err := e.planSessionDeletion(ctx, sessionLocks, options.DryRun)
	if err != nil {
		return result, err
	}
	result.Sessions, err = e.removeSessionState(ctx, planned, options.DryRun)
	retained := []string{}
	for _, name := range result.Retained {
		if !slices.Contains(result.Sessions, name) {
			retained = append(retained, name)
		}
	}
	result.Retained = retained
	return result, err
}
