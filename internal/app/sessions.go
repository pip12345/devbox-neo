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

type SessionDetails struct {
	Record    store.Record       `json:"record"`
	Container View               `json:"container"`
	Active    []store.Lease      `json:"active"`
	Pending   *store.Reservation `json:"pending_transfer,omitempty"`
}

func (e *Engine) SessionShow(ctx context.Context, target, profile string) (SessionDetails, error) {
	r, err := e.Locate(ctx, target, profile)
	if errors.Is(err, os.ErrNotExist) && strings.HasPrefix(target, environment.ContainerPrefix) && !strings.ContainsAny(target, "/\\") {
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
			return nil, commanderror.New("container_present", "Delete the container before deleting its session.", r.Identity.Name, nil,
				commanderror.Next("Delete container", "delete", r.Identity.Name, "--container"))
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
			if err := item.lock.Delete(); err != nil {
				return removed, err
			}
			removed = append(removed, item.record.Identity.Name)
			if item.tagged {
				if err := e.Docker.Untag(ctx, item.record.ImageTag, item.record.ImageID, e.Store.Installation); err != nil {
					return removed, fmt.Errorf("session state removed but image-tag cleanup failed: %w", err)
				}
			}
		} else {
			removed = append(removed, item.record.Identity.Name)
		}
	}
	return removed, nil
}
