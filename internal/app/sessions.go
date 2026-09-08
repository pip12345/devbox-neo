package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/harness"
	"devbox/internal/store"
)

type SessionDetails struct {
	Record    store.Record       `json:"record"`
	Container View               `json:"container"`
	Active    []store.Lease      `json:"active"`
	Pending   *store.Reservation `json:"pending_transfer,omitempty"`
}

func (e *Engine) SessionShow(ctx context.Context, target, profile string) (SessionDetails, error) {
	r, err := e.Locate(ctx, target, profile)
	if os.IsNotExist(err) && strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\") {
		pending, pendingErr := e.Store.Pending(target)
		if pendingErr != nil {
			return SessionDetails{}, pendingErr
		}
		if pending != nil {
			return SessionDetails{Container: View{Name: target, Pending: pending}, Pending: pending, Active: []store.Lease{}}, nil
		}
	}
	if err != nil {
		return SessionDetails{}, err
	}
	lock, err := e.Store.Lock(ctx, r.Identity.Name)
	if err != nil {
		return SessionDetails{}, err
	}
	defer lock.Close()
	r, err = lock.ReadRecord(ctx)
	if err != nil {
		return SessionDetails{}, err
	}
	pending, err := e.Store.Pending(r.Identity.Name)
	if err != nil {
		return SessionDetails{}, err
	}
	leases, err := lock.LiveLeases()
	if err != nil {
		return SessionDetails{}, err
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return SessionDetails{}, err
	}
	view := recordView(r)
	view.Exists = exists
	view.Running = exists && c.State.Running
	if exists {
		view.ContainerID = c.ID
	}
	view.Pending = pending
	return SessionDetails{Record: r, Container: view, Active: leases, Pending: pending}, nil
}
func (e *Engine) sessionNames(ctx context.Context, targets []string, profile string, all bool) ([]string, error) {
	if all && len(targets) > 0 {
		return nil, fmt.Errorf("--all cannot be combined with exact session targets")
	}
	names := map[string]bool{}
	if all {
		entries, err := e.Store.Inventory(ctx)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Err != nil {
				return nil, fmt.Errorf("invalid session %s: %w", entry.Name, entry.Err)
			}
			if profile == "" || entry.Record.Identity.Profile == profile {
				names[entry.Name] = true
			}
		}
	} else {
		if len(targets) == 0 {
			return nil, fmt.Errorf("provide at least one exact session target")
		}
		for _, target := range targets {
			r, err := e.Locate(ctx, target, profile)
			if err != nil {
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

type ResetOptions struct {
	Targets        []string
	Profile        string
	All            bool
	Harness        string
	AllHarnesses   bool
	IncludeHistory bool
	DryRun         bool
}
type ResetResult struct {
	Name    string   `json:"name"`
	Removed []string `json:"removed"`
	DryRun  bool     `json:"dry_run"`
}

func (e *Engine) ResetSessions(ctx context.Context, options ResetOptions) ([]ResetResult, error) {
	if options.Harness != "" && options.AllHarnesses {
		return nil, fmt.Errorf("select --harness or --all-harnesses")
	}
	if options.Harness != "" && !config.Name.MatchString(options.Harness) {
		return nil, fmt.Errorf("invalid harness name")
	}
	names, err := e.sessionNames(ctx, options.Targets, options.Profile, options.All)
	if err != nil {
		return nil, err
	}
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return nil, err
	}
	defer store.CloseAll(locks)
	type reset struct {
		record store.Record
		lock   *store.Locked
		paths  []string
	}
	planned := []reset{}
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
		if exists && c.State.Running {
			return nil, fmt.Errorf("session %s must be stopped before reset", r.Identity.Name)
		}
		harnesses := []string{r.Definition.Name}
		if options.Harness != "" {
			harnesses = []string{options.Harness}
		}
		if options.AllHarnesses {
			root, err := lock.Path("harnesses")
			if err != nil {
				return nil, err
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				return nil, err
			}
			harnesses = nil
			for _, entry := range entries {
				if !entry.IsDir() || !config.Name.MatchString(entry.Name()) {
					return nil, fmt.Errorf("invalid harness state entry")
				}
				harnesses = append(harnesses, entry.Name())
			}
		}
		paths := []string{}
		for _, name := range harnesses {
			preserve := []string{}
			if !options.IncludeHistory {
				effective, err := harness.Load(e.Store.Home, name)
				if err != nil {
					return nil, err
				}
				preserve = effective.Definition.Session.Preserve
			}
			base := filepath.Join("harnesses", name)
			root, err := lock.Path(filepath.Join(base, "stores"))
			if err != nil {
				return nil, err
			}
			stores, err := os.ReadDir(root)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range stores {
				if !entry.IsDir() || !config.Name.MatchString(entry.Name()) {
					return nil, fmt.Errorf("invalid environment store")
				}
				storeRoot, err := lock.Path(filepath.Join(base, "stores", entry.Name()))
				if err != nil {
					return nil, err
				}
				remove, _, err := resetPaths(storeRoot, ".", preserve)
				if err != nil {
					return nil, err
				}
				paths = append(paths, remove...)
			}
		}
		planned = append(planned, reset{r, lock, paths})
	}
	results := []ResetResult{}
	for _, item := range planned {
		result := ResetResult{Name: item.record.Identity.Name, DryRun: options.DryRun, Removed: []string{}}
		root, err := item.lock.Path(".")
		if err != nil {
			return results, err
		}
		for _, p := range item.paths {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return results, err
			}
			result.Removed = append(result.Removed, rel)
			if !options.DryRun {
				if err = ctx.Err(); err != nil {
					return results, err
				}
				if err = os.RemoveAll(p); err != nil {
					return results, err
				}
			}
		}
		if !options.DryRun {
			item.record.Action = "reset"
			item.record.Activity = time.Now().UTC()
			if err = item.lock.Save(item.record); err != nil {
				return results, err
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// The store root remains bound to Docker. Only descendants are removed, and
// symlinks are unlinked as entries rather than traversed into external state.
func resetPaths(root, rel string, preserve []string) ([]string, bool, error) {
	for _, pattern := range preserve {
		matched, err := filepath.Match(pattern, rel)
		if err != nil {
			return nil, false, err
		}
		if matched {
			return nil, true, nil
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		return nil, false, err
	}
	removed := []string{}
	kept := false
	for _, entry := range entries {
		child := filepath.Join(rel, entry.Name())
		preserved := false
		for _, pattern := range preserve {
			matched, err := filepath.Match(pattern, child)
			if err != nil {
				return nil, false, err
			}
			if matched {
				preserved = true
				break
			}
		}
		if preserved {
			kept = true
			continue
		}
		full := filepath.Join(root, child)
		if entry.IsDir() {
			paths, childKept, err := resetPaths(root, child, preserve)
			if err != nil {
				return nil, false, err
			}
			if childKept {
				removed = append(removed, paths...)
				kept = true
			} else {
				removed = append(removed, full)
			}
		} else {
			removed = append(removed, full)
		}
	}
	return removed, kept, nil
}

type PruneOptions struct {
	Orphaned  bool
	OlderThan time.Duration
	DryRun    bool
	Confirm   bool
	Profile   string
}

func (e *Engine) PruneSessions(ctx context.Context, options PruneOptions) ([]string, error) {
	if options.OlderThan < 0 {
		return nil, fmt.Errorf("age must not be negative")
	}
	if !options.Orphaned && options.OlderThan == 0 {
		return nil, fmt.Errorf("provide --orphaned or --older-than")
	}
	if !options.DryRun && !options.Confirm {
		return nil, fmt.Errorf("filtered state deletion requires confirmation.\nPreview with --dry-run.\nThen repeat with --yes to confirm deletion.")
	}
	views, err := e.List(ctx, true)
	if err != nil {
		return nil, err
	}
	selected := []string{}
	cutoff := time.Now().Add(-options.OlderThan)
	for _, view := range views {
		if view.Error != "" {
			return nil, fmt.Errorf("cannot classify session %s: %s", view.Name, view.Error)
		}
		if options.Profile != "" && view.Profile != options.Profile {
			continue
		}
		if options.Orphaned && view.Exists {
			continue
		}
		if options.OlderThan > 0 && !view.LastActivity.Before(cutoff) {
			continue
		}
		selected = append(selected, view.Name)
	}
	sort.Strings(selected)
	if options.OlderThan == 0 {
		cutoff = time.Time{}
	}
	return e.deleteSessionNames(ctx, selected, options.DryRun, cutoff)
}
func (e *Engine) DeleteSessions(ctx context.Context, targets []string, profile string, dryRun bool) ([]string, error) {
	names, err := e.sessionNames(ctx, targets, profile, false)
	if err != nil {
		return nil, err
	}
	return e.deleteSessionNames(ctx, names, dryRun, time.Time{})
}
func (e *Engine) deleteSessionNames(ctx context.Context, names []string, dryRun bool, before time.Time) ([]string, error) {
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		return nil, err
	}
	defer store.CloseAll(locks)
	type deletion struct {
		record store.Record
		lock   *store.Locked
		tagged bool
	}
	planned := []deletion{}
	for _, lock := range locks {
		r, err := lock.Load()
		if err != nil {
			return nil, err
		}
		if err = lock.RequireIdle(); err != nil {
			return nil, err
		}
		if !before.IsZero() && !r.Activity.Before(before) {
			return nil, fmt.Errorf("session activity changed since selection.\nPreview cleanup again.")
		}
		_, exists, err := e.inspect(ctx, r)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("session %s still has a container.\n\nDelete the container first:\n  devbox-neo delete %s\nThen retry session deletion.", r.Identity.Name, r.Identity.Name)
		}
		image, tagged, err := e.Docker.TaggedImage(ctx, r.ImageTag)
		if err != nil {
			return nil, err
		}
		if tagged {
			if err = image.Verify(e.Store.Installation); err != nil {
				return nil, err
			}
			if image.ID != r.ImageID {
				return nil, fmt.Errorf("session image tag points to another image")
			}
		}
		planned = append(planned, deletion{r, lock, tagged})
	}
	removed := []string{}
	for _, item := range planned {
		if !dryRun {
			if err = ctx.Err(); err != nil {
				return removed, err
			}
			if err = item.lock.Delete(); err != nil {
				return removed, err
			}
			removed = append(removed, item.record.Identity.Name)
			if item.tagged {
				if err = e.Docker.Untag(ctx, item.record.ImageTag, item.record.ImageID, e.Store.Installation); err != nil {
					return removed, fmt.Errorf("session state removed but image-tag cleanup failed: %w", err)
				}
			}
		} else {
			removed = append(removed, item.record.Identity.Name)
		}
	}
	return removed, nil
}
