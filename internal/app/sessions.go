package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
)

type StatusDetails struct {
	View
	Record       *store.Record `json:"record,omitempty"`
	Active       []store.Lease `json:"active"`
	DefaultError string        `json:"default_error,omitempty"`
}

func (e *Engine) Status(ctx context.Context, target, localName string) (StatusDetails, error) {
	r, err := e.Locate(ctx, target, localName)
	if errors.Is(err, os.ErrNotExist) && environment.IsSessionTarget(target) {
		pending, pendingErr := e.Store.PendingID(target)
		if pendingErr != nil {
			return StatusDetails{}, pendingErr
		}
		if pending != nil {
			return StatusDetails{View: View{Target: target, Pending: pending}, Active: []store.Lease{}}, nil
		}
	}
	if err != nil {
		return StatusDetails{}, err
	}
	lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		return StatusDetails{}, err
	}
	defer lock.Close()
	selectedID := r.ID
	r, err = lock.ReadRecord(ctx)
	if err != nil {
		return StatusDetails{}, err
	}
	if r.ID != selectedID {
		return StatusDetails{}, fmt.Errorf("selected session changed; retry status")
	}
	pending, err := e.Store.Pending(r.Directory)
	if err != nil {
		return StatusDetails{}, err
	}
	leases, err := lock.LiveLeases()
	if err != nil {
		return StatusDetails{}, err
	}
	if leases == nil {
		leases = []store.Lease{}
	}
	c, exists, err := e.inspect(ctx, r)
	if err != nil {
		return StatusDetails{}, err
	}
	view := recordView(r)
	view.Exists = exists
	view.Running = exists && c.State.Running
	if exists {
		view.ContainerID = c.ID
		view.ContainerName = strings.TrimPrefix(c.Name, "/")
		view.CreatedAt = c.Created
	}
	view.Pending = pending
	if pending == nil {
		e.desiredStatus(&view, r)
	}
	details := StatusDetails{View: view, Record: &r, Active: leases}
	selected, defaultErr := e.Store.ReadDefault(ctx, r.Settings.Workspace)
	if defaultErr != nil {
		if errors.Is(defaultErr, context.Canceled) || errors.Is(defaultErr, context.DeadlineExceeded) {
			return StatusDetails{}, defaultErr
		}
		// Default metadata must not hide an explicitly selected session's
		// saved details, container state, or configuration diagnostics.
		details.DefaultError = defaultErr.Error()
	} else {
		details.Default = selected != nil && selected.ID == r.ID
	}
	return details, nil
}

type sessionRemoval struct {
	record store.Record
	lock   *store.Locked
	tagged bool
}

func (e *Engine) planSessionDeletion(ctx context.Context, locks []*store.Locked, removingContainers bool) ([]sessionRemoval, error) {
	planned := []sessionRemoval{}
	for _, lock := range locks {
		r, err := lock.Load()
		if err != nil {
			return nil, err
		}
		if err = lock.RequireIdle(); err != nil {
			return nil, err
		}
		_, exists, err := e.inspect(ctx, r)
		if err != nil {
			return nil, err
		}
		if exists && !removingContainers {
			return nil, commanderror.New("container_present", "Delete the container before deleting its session.", r.ID, nil,
				commanderror.Next("Delete container", "delete", r.ID, "--container"))
		}
		image, tagged, err := e.Docker.TaggedImage(ctx, r.Applied.ImageTag)
		if err != nil {
			return nil, err
		}
		if tagged {
			if err = image.Verify(e.Store.Installation); err != nil {
				return nil, err
			}
			if image.ID != r.Applied.ImageID {
				return nil, fmt.Errorf("session image tag points to another image")
			}
		}
		planned = append(planned, sessionRemoval{r, lock, tagged})
	}
	return planned, nil
}

func (e *Engine) removeSessionState(ctx context.Context, planned []sessionRemoval, dryRun bool) ([]string, error) {
	removed := []string{}
	for _, item := range planned {
		if !dryRun {
			if err := ctx.Err(); err != nil {
				return removed, err
			}
			if err := removeSavedSession(ctx, item.lock, item.record.Settings.Workspace, item.record.ID); err != nil {
				return removed, err
			}
			removed = append(removed, item.record.ID)
			if item.tagged {
				if err := e.Docker.Untag(ctx, item.record.Applied.ImageTag, item.record.Applied.ImageID, e.Store.Installation); err != nil {
					return removed, fmt.Errorf("session state removed but image-tag cleanup failed: %w", err)
				}
			}
		} else {
			removed = append(removed, item.record.ID)
		}
	}
	return removed, nil
}

// Both whole-session deletion and committed move cleanup clear the default
// before deleting state. The operation lock stays held across both writes.
func removeSavedSession(ctx context.Context, lock *store.Locked, workspace, id string) error {
	if err := lock.ClearMatchingDefault(ctx, workspace, id); err != nil {
		return err
	}
	if err := lock.DeleteContext(ctx); err != nil {
		return commanderror.New("session_delete_incomplete", "Saved session removal failed; its default may already be cleared.", lock.Name, err,
			commanderror.Next("Inspect retained state", "status", lock.Name))
	}
	return nil
}
