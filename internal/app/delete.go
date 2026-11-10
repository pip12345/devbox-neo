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
	targets := map[string]SelectedTarget{}
	for _, view := range append(report.Sessions, report.UnmatchedContainers...) {
		if view.Uncommitted {
			continue
		}
		if selection.Stopped && (!view.Exists || view.Running) || options.Orphaned && view.Exists {
			continue
		}
		target := SelectedTarget{name: view.Target, sessionID: view.OwnerSessionID, containerID: view.ContainerID, unmatched: true}
		if !cutoff.IsZero() && (view.Error != "" || view.LastActivity.IsZero()) {
			target.err = fmt.Errorf("cannot determine last activity for %s; inspect this environment before filtered deletion", view.Target)
		} else if !cutoff.IsZero() && !view.LastActivity.Before(cutoff) {
			continue
		} else if view.Pending != nil {
			target.err = commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", view.Target, nil, view.Pending.RetryStep())
		} else if view.SessionID != "" {
			r, err := e.Store.Find(ctx, view.SessionID, nil)
			if err != nil {
				target.err = err
			} else {
				target = selectedRecord(r)
			}
		} else if view.Error != "" && view.ContainerID == "" {
			target.err = commanderror.New("invalid_session_record", "Cannot select unreadable session state for deletion.", view.Target, nil)
		}
		targets[target.name] = target
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

type deletePlan struct {
	lock      *store.Locked
	container string
	saved     bool
	effect    DeleteEffect
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
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	needsNamespace := false
	for _, target := range targets {
		needsNamespace = needsNamespace || target.incomplete != nil && target.err == nil
	}
	if needsNamespace {
		namespace, lockErr := e.Store.LockNames(ctx)
		if lockErr != nil {
			for i := range targets {
				if targets[i].incomplete != nil && targets[i].err == nil {
					targets[i].err = lockErr
				}
			}
		} else {
			defer fsutil.Unlock(namespace)
		}
	}
	// A shared owner can have several unmatched instances. Acquire each ID
	// once, in ID order, while keeping target-local acquisition failures local.
	ids := map[string]bool{}
	for _, target := range targets {
		if target.err == nil && target.incomplete == nil {
			ids[target.sessionID] = true
		}
	}
	orderedIDs := make([]string, 0, len(ids))
	for id := range ids {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Strings(orderedIDs)
	locks := map[string]*store.Locked{}
	var held []*store.Locked
	defer func() { store.CloseAll(held) }()
	for _, id := range orderedIDs {
		lock, lockErr := e.Store.Lock(ctx, id)
		if lockErr != nil {
			for i := range targets {
				if targets[i].sessionID == id && targets[i].err == nil {
					targets[i].err = lockErr
				}
			}
		} else {
			locks[id] = lock
			held = append(held, lock)
		}
		if ctx.Err() != nil {
			break
		}
	}
	b := newBatch[DeleteEffect](targets)
	plans := map[string]*deletePlan{}
	key := func(target SelectedTarget) string {
		if target.incomplete != nil {
			return "directory:" + target.incomplete.Path
		}
		if target.unmatched {
			return "container:" + target.containerID
		}
		return "session:" + target.sessionID
	}
	defer func() {
		for i := range b.report.Results {
			item := &b.report.Results[i]
			if item.Status == BatchSucceeded && item.Result != nil && item.Result.deletedCount() == 0 {
				item.Status = BatchUnchanged
			}
		}
		result.Targets, result.Batch = targets, b.report
		for _, item := range b.report.Results {
			if item.Result == nil {
				continue
			}
			v := item.Result
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
		err = errors.Join(err, b.err(ctx))
	}()
	b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[DeleteEffect]) error {
		p := &deletePlan{lock: locks[target.sessionID]}
		plans[key(target)] = p
		item.Result = &p.effect
		if target.incomplete != nil {
			p.effect.RetainedIncompleteDirectory = target.name
			if options.Scope == DeleteSession {
				return e.checkIncompleteDeletion(ctx, target.incomplete)
			}
			return nil
		}
		r, loadErr := p.lock.Load()
		if loadErr == nil && !target.unmatched {
			p.saved, p.effect.RetainedSession = true, r.PublicName()
			item.Session = r.PublicName()
		} else if loadErr != nil && !os.IsNotExist(loadErr) {
			return loadErr
		}
		if err := e.recheckDeleteFilters(ctx, p.lock, options, cutoff); err != nil {
			return err
		}
		var err error
		p.container, err = e.deleteContainerLocked(ctx, target, options.Selection, p.lock, options.Force, true)
		if err != nil {
			return err
		}
		if options.Scope == DeleteSession && p.saved {
			_, err = e.planSessionDeletion(ctx, p.lock, true)
		}
		return err
	})
	prompt := func(saved bool) DeletePrompt {
		var prompt DeletePrompt
		for i, item := range b.report.Results {
			if item.Result != nil && item.Result.Container != "" {
				prompt.RemovedContainers = append(prompt.RemovedContainers, item.Result.Container)
			}
			if item.Err != nil || item.Status == BatchSkipped {
				continue
			}
			p := plans[key(targets[i])]
			if saved {
				if p.saved {
					prompt.Sessions = append(prompt.Sessions, p.effect.RetainedSession)
				}
				if p.effect.RetainedIncompleteDirectory != "" {
					prompt.IncompleteDirectories = append(prompt.IncompleteDirectories, p.effect.RetainedIncompleteDirectory)
				}
			} else if p.container != "" {
				prompt.Containers = append(prompt.Containers, p.container)
			}
		}
		return prompt
	}
	skip := func(reason string) {
		b.run(ctx, func(_ context.Context, _ SelectedTarget, item *BatchResult[DeleteEffect]) error {
			item.Status, item.Reason = BatchSkipped, reason
			return nil
		})
	}
	if ctx.Err() != nil {
		skip("Not attempted: batch cancelled.")
		return result, nil
	}
	containers := prompt(false)
	if options.Confirm != nil && !options.DryRun && len(containers.Containers) > 0 {
		ok, confirmErr := options.Confirm(containers)
		if confirmErr != nil || !ok {
			skip("Container removal not confirmed.")
			result.Cancelled = !ok && confirmErr == nil
			return result, confirmErr
		}
	}
	b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[DeleteEffect]) error {
		if target.incomplete != nil {
			item.complete = options.Scope == DeleteContainer
			return nil
		}
		p := plans[key(target)]
		if err := e.recheckDeleteFilters(ctx, p.lock, options, cutoff); err != nil {
			return err
		}
		var err error
		p.effect.Container, err = e.deleteContainerLocked(ctx, target, options.Selection, p.lock, options.Force, options.DryRun)
		if err == nil {
			item.complete = options.Scope == DeleteContainer || !p.saved
		}
		return err
	})
	if ctx.Err() != nil {
		skip("Not completed: batch cancelled.")
		return result, nil
	}
	include := options.Scope == DeleteSession
	saved := prompt(true)
	if options.Confirm != nil && !options.DryRun && options.Scope != DeleteContainer && len(saved.Sessions)+len(saved.IncompleteDirectories) > 0 {
		var confirmErr error
		include, confirmErr = options.Confirm(saved)
		if confirmErr != nil {
			skip("Saved-data removal not confirmed.")
			return result, confirmErr
		}
	}
	if include {
		b.run(ctx, func(ctx context.Context, target SelectedTarget, item *BatchResult[DeleteEffect]) error {
			p := plans[key(target)]
			if target.incomplete != nil {
				if err := e.checkIncompleteDeletion(ctx, target.incomplete); err != nil {
					return err
				}
				if !options.DryRun {
					if err := target.incomplete.Remove(ctx); err != nil {
						return err
					}
				}
				p.effect.IncompleteDirectory, p.effect.RetainedIncompleteDirectory = target.name, ""
				item.complete = true
				return nil
			}
			if !p.saved {
				return nil
			}
			// Unmatched instances can share this session's stores. A failed
			// selected runtime must not lose its backing data through another
			// target that holds the same session ownership.
			for i, other := range targets {
				outcome := b.report.Results[i]
				if other.sessionID == target.sessionID && other.containerID != "" && outcome.Err != nil && (outcome.Result == nil || outcome.Result.Container == "") {
					return commanderror.New("container_cleanup_incomplete", "A selected container owned by this session was not removed. Saved data is retained.", item.Session, outcome.Err)
				}
			}
			planned, err := e.planSessionDeletion(ctx, p.lock, options.DryRun)
			if err != nil {
				return err
			}
			removed, err := e.removeSessionState(ctx, planned, options.DryRun)
			if removed != "" {
				p.effect.Session, p.effect.RetainedSession = removed, ""
			}
			item.complete = err == nil
			return err
		})
	}
	return result, nil
}
