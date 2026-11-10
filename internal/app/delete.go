package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

type DeletePrompt struct {
	Containers            []string
	Sessions              []string
	IncompleteDirectories []string
	RemovedContainers     []string
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

// DeleteEffect records confirmed effects for one target, including progress
// retained when a later phase or image-tag cleanup fails.
type DeleteEffect struct {
	Container                   string `json:"container,omitempty"`
	Session                     string `json:"session,omitempty"`
	RetainedSession             string `json:"retained_session,omitempty"`
	IncompleteDirectory         string `json:"incomplete_directory,omitempty"`
	RetainedIncompleteDirectory string `json:"retained_incomplete_directory,omitempty"`
}

func (e DeleteEffect) deletedCount() int {
	n := 0
	for _, name := range []string{e.Container, e.Session, e.IncompleteDirectory} {
		if name != "" {
			n++
		}
	}
	return n
}

type DeleteResult struct {
	// Targets retains captured identities across a preview and a later form.
	Targets                       []SelectedTarget          `json:"-"`
	Containers                    []string                  `json:"containers"`
	Sessions                      []string                  `json:"sessions"`
	Retained                      []string                  `json:"retained_sessions"`
	DryRun                        bool                      `json:"dry_run"`
	Cancelled                     bool                      `json:"cancelled"`
	IncompleteDirectories         []string                  `json:"incomplete_directories,omitempty"`
	RetainedIncompleteDirectories []string                  `json:"retained_incomplete_directories,omitempty"`
	Batch                         BatchReport[DeleteEffect] `json:"-"`
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
	if len(selection.Targets) > 0 || len(selection.Captured) > 0 || len(selection.ContainerNames)+len(selection.IncompleteDirs) > 0 || selection.LocalName != "" {
		return nil, fmt.Errorf("use exact targets with optional --name, or selection filters, not both")
	}
	report, err := e.List(ctx, "")
	if err != nil {
		return nil, err
	}
	targets := map[string]targetSelection{}
	for _, view := range append(report.Sessions, report.UnmatchedContainers...) {
		if view.Uncommitted {
			continue
		}
		if selection.Stopped && (!view.Exists || view.Running) || options.Orphaned && view.Exists {
			continue
		}
		selected := targetSelection{target: SelectedTarget{name: view.Target, sessionID: view.OwnerSessionID, containerID: view.ContainerID, unmatched: true}}
		if view.SessionID != "" {
			selected.target = SelectedTarget{name: view.Target, sessionID: view.SessionID, record: recordPresent}
		}
		if !cutoff.IsZero() && (view.Error != "" || view.LastActivity.IsZero()) {
			selected.err = fmt.Errorf("cannot determine last activity for %s; inspect this environment before filtered deletion", view.Target)
		} else if !cutoff.IsZero() && !view.LastActivity.Before(cutoff) {
			continue
		} else if view.Pending != nil {
			selected.err = commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", view.Target, nil, view.Pending.RetryStep())
		} else if view.SessionID != "" {
			r, err := e.Store.Find(ctx, view.SessionID, nil)
			selected.err = err
			if err == nil {
				selected.target = selectedRecord(r)
			}
		} else if view.Error != "" && view.ContainerID == "" {
			selected.err = commanderror.New("invalid_session_record", "Cannot select unreadable session state for deletion.", view.Target, nil)
		}
		targets[selected.target.name] = selected
	}
	return orderedTargets(targets), nil
}

// Recheck filters under the target's lock, including after confirmation and
// before container deletion records its own new activity timestamp.
func (e *Engine) recheckDeleteFilters(ctx context.Context, lock *store.Locked, options DeleteOptions, cutoff time.Time) error {
	if cutoff.IsZero() && !options.Orphaned {
		return nil
	}
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
	return nil
}

type deleteWorkItem struct {
	batchEntry[DeleteEffect]
	lock      *store.Locked
	container string
	saved     bool
	done      bool
}

func pendingDeletion(items []*deleteWorkItem) []*deleteWorkItem {
	pending := make([]*deleteWorkItem, 0, len(items))
	for _, item := range items {
		if !item.done {
			pending = append(pending, item)
		}
	}
	return pending
}

func skipDeletion(items []*deleteWorkItem, reason string) {
	for _, item := range pendingDeletion(items) {
		if item.outcome.Err == nil {
			item.outcome.Status, item.outcome.Reason = BatchSkipped, reason
		}
	}
}

func deletionPrompt(items []*deleteWorkItem, saved bool) DeletePrompt {
	var prompt DeletePrompt
	for _, item := range items {
		effect := item.outcome.Result
		if effect != nil && effect.Container != "" {
			prompt.RemovedContainers = append(prompt.RemovedContainers, effect.Container)
		}
		if item.outcome.Err != nil || item.outcome.Status == BatchSkipped {
			continue
		}
		if saved {
			if item.saved {
				prompt.Sessions = append(prompt.Sessions, effect.RetainedSession)
			}
			if effect.RetainedIncompleteDirectory != "" {
				prompt.IncompleteDirectories = append(prompt.IncompleteDirectories, effect.RetainedIncompleteDirectory)
			}
		} else if item.container != "" {
			prompt.Containers = append(prompt.Containers, item.container)
		}
	}
	return prompt
}

// Deletion retains namespace/session locks through both confirmations and
// phases. Failures disqualify only their target, never authorize wider cleanup.
func (e *Engine) Delete(ctx context.Context, options DeleteOptions) (result DeleteResult, err error) {
	result = DeleteResult{Containers: []string{}, Sessions: []string{}, Retained: []string{}, DryRun: options.DryRun}
	if options.Scope != "" && options.Scope != DeleteContainer && options.Scope != DeleteSession {
		return result, fmt.Errorf("invalid deletion scope")
	}
	if options.Scope == "" && (options.Confirm == nil || options.DryRun) {
		return result, fmt.Errorf("choose container or session deletion scope")
	}
	if len(options.Selection.ContainerNames) > 0 && options.Scope != DeleteContainer {
		return result, fmt.Errorf("--container-name requires --container; it never selects saved session data")
	}
	if len(options.Selection.IncompleteDirs) > 0 && options.Scope != DeleteSession {
		return result, fmt.Errorf("--incomplete-dir requires --session")
	}
	if options.OlderThan < 0 {
		return result, fmt.Errorf("age must be positive")
	}
	if len(options.Selection.Targets) > 0 && len(options.Selection.Captured) > 0 {
		return result, fmt.Errorf("use requested or captured targets, not both")
	}
	var cutoff time.Time
	if options.OlderThan > 0 {
		cutoff = time.Now().Add(-options.OlderThan)
	}
	incomplete, remaining, err := e.incompleteDeletionTargets(ctx, options)
	if err != nil {
		return result, err
	}
	var targets selectedTargets
	if len(options.Selection.IncompleteDirs) == 0 && len(incomplete) == 0 || len(remaining.Targets)+len(remaining.Captured)+len(remaining.ContainerNames) > 0 {
		q := options
		q.Selection = remaining
		targets, err = e.deletionTargets(ctx, q, cutoff)
		if err != nil {
			return result, err
		}
	}
	targets = append(targets, incomplete...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].target.name < targets[j].target.name })
	items := make([]*deleteWorkItem, 0, len(targets))
	owners := map[string][]*deleteWorkItem{}
	needsNamespace := false
	for _, selection := range targets {
		item := &deleteWorkItem{batchEntry: newBatchEntry[DeleteEffect](selection)}
		items = append(items, item)
		if item.target.incomplete != nil {
			needsNamespace = needsNamespace || item.outcome.Err == nil
		} else if item.target.sessionID != "" {
			owners[item.target.sessionID] = append(owners[item.target.sessionID], item)
		}
	}
	if needsNamespace {
		namespace, lockErr := e.Store.LockNames(ctx)
		if lockErr != nil {
			for _, item := range items {
				if item.target.incomplete != nil && item.outcome.Err == nil {
					item.outcome.Err = lockErr
				}
			}
		} else {
			defer fsutil.Unlock(namespace)
		}
	}
	// Keep failures in owner groups even when they cannot run: an unremoved
	// selected runtime must still protect stores shared with that owner.
	var ids []string
	for id, group := range owners {
		for _, item := range group {
			if item.outcome.Err == nil {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	var held []*store.Locked
	defer func() { store.CloseAll(held) }()
	for _, id := range ids {
		lock, lockErr := e.Store.Lock(ctx, id)
		if lockErr == nil {
			held = append(held, lock)
		}
		for _, item := range owners[id] {
			if item.outcome.Err == nil {
				item.lock, item.outcome.Err = lock, lockErr
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	defer func() {
		for _, item := range items {
			// Do not turn an unresolved name into authority for a later retry.
			if item.target.sessionID != "" || item.target.incomplete != nil {
				result.Targets = append(result.Targets, item.target)
			}
			v := item.outcome.Result
			if v == nil {
				continue
			}
			if item.outcome.Err == nil && item.outcome.Status == "" && v.deletedCount() == 0 {
				item.outcome.Status = BatchUnchanged
			}
			if v.Container != "" {
				result.Containers = append(result.Containers, v.Container)
			}
			if v.Session != "" {
				result.Sessions = append(result.Sessions, v.Session)
			}
			if v.RetainedSession != "" {
				result.Retained = append(result.Retained, v.RetainedSession)
			}
			if v.IncompleteDirectory != "" {
				result.IncompleteDirectories = append(result.IncompleteDirectories, v.IncompleteDirectory)
			}
			if v.RetainedIncompleteDirectory != "" {
				result.RetainedIncompleteDirectories = append(result.RetainedIncompleteDirectories, v.RetainedIncompleteDirectory)
			}
		}
		var batchErr error
		result.Batch, batchErr = reportBatch(ctx, items)
		err = errors.Join(err, batchErr)
	}()
	runBatch(ctx, items, func(ctx context.Context, item *deleteWorkItem) error {
		target := item.target
		item.outcome.Result = &DeleteEffect{}
		effect := item.outcome.Result
		if target.incomplete != nil {
			effect.RetainedIncompleteDirectory = target.name
			if options.Scope == DeleteSession {
				return e.checkIncompleteDeletion(ctx, target.incomplete)
			}
			return nil
		}
		r, loadErr := item.lock.Load()
		if loadErr == nil && !target.unmatched {
			item.saved, effect.RetainedSession = true, r.PublicName()
			item.outcome.Session = r.PublicName()
		} else if loadErr != nil && !os.IsNotExist(loadErr) {
			return loadErr
		}
		if err := e.recheckDeleteFilters(ctx, item.lock, options, cutoff); err != nil {
			return err
		}
		var err error
		item.container, err = e.deleteContainerLocked(ctx, target, options.Selection, item.lock, options.Force, true)
		if err == nil && options.Scope == DeleteSession && item.saved {
			_, err = e.planSessionDeletion(ctx, item.lock, true)
		}
		return err
	})
	if ctx.Err() != nil {
		skipDeletion(items, "Not completed: batch cancelled.")
		return result, nil
	}
	containers := deletionPrompt(items, false)
	if options.Confirm != nil && !options.DryRun && len(containers.Containers) > 0 {
		ok, confirmErr := options.Confirm(containers)
		if confirmErr != nil || !ok {
			skipDeletion(items, "Container removal not confirmed.")
			result.Cancelled = !ok && confirmErr == nil
			return result, confirmErr
		}
	}
	runBatch(ctx, pendingDeletion(items), func(ctx context.Context, item *deleteWorkItem) error {
		if item.target.incomplete != nil {
			item.done = options.Scope == DeleteContainer
			return nil
		}
		if err := e.recheckDeleteFilters(ctx, item.lock, options, cutoff); err != nil {
			return err
		}
		var err error
		item.outcome.Result.Container, err = e.deleteContainerLocked(ctx, item.target, options.Selection, item.lock, options.Force, options.DryRun)
		if err == nil {
			item.done = options.Scope == DeleteContainer || !item.saved
		}
		return err
	})
	if ctx.Err() != nil {
		skipDeletion(items, "Not completed: batch cancelled.")
		return result, nil
	}
	include := options.Scope == DeleteSession
	saved := deletionPrompt(items, true)
	if options.Confirm != nil && !options.DryRun && options.Scope != DeleteContainer && len(saved.Sessions)+len(saved.IncompleteDirectories) > 0 {
		var confirmErr error
		include, confirmErr = options.Confirm(saved)
		if confirmErr != nil {
			skipDeletion(items, "Saved-data removal not confirmed.")
			return result, confirmErr
		}
	}
	if include {
		runBatch(ctx, pendingDeletion(items), func(ctx context.Context, item *deleteWorkItem) error {
			target, effect := item.target, item.outcome.Result
			if target.incomplete != nil {
				if err := e.checkIncompleteDeletion(ctx, target.incomplete); err != nil {
					return err
				}
				if !options.DryRun {
					if err := target.incomplete.Remove(ctx); err != nil {
						return err
					}
				}
				effect.IncompleteDirectory, effect.RetainedIncompleteDirectory = target.name, ""
				item.done = true
				return nil
			}
			if !item.saved {
				return nil
			}
			for _, other := range owners[target.sessionID] {
				if other.target.containerID != "" && other.outcome.Err != nil && (other.outcome.Result == nil || other.outcome.Result.Container == "") {
					return commanderror.New("container_cleanup_incomplete", "A selected container owned by this session was not removed. Saved data is retained.", item.outcome.Session, other.outcome.Err)
				}
			}
			planned, err := e.planSessionDeletion(ctx, item.lock, options.DryRun)
			if err != nil {
				return err
			}
			removed, err := e.removeSessionState(ctx, planned, options.DryRun)
			if removed != "" {
				effect.Session, effect.RetainedSession = removed, ""
			}
			item.done = err == nil
			return err
		})
	}
	return result, nil
}
