package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// Create prepares a new environment without attaching a harness. Preparation
// requires a running container, but successful standalone creation leaves it stopped.
func (e *Engine) Create(ctx context.Context, q Request) (Result, error) {
	spec, err := e.Resolve(q)
	if err != nil {
		return Result{}, err
	}
	result := Result{Name: spec.Identity.Name}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if _, err = lock.Load(); err == nil {
		return result, commanderror.New("session_exists", "Environment already exists.", result.Name, nil,
			commanderror.Next("Open", "open", result.Name),
			commanderror.Next("Or recreate with current configuration", "recreate", result.Name))
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = e.requireNew(ctx, lock); err != nil {
		return result, err
	}
	record, c, err := e.create(ctx, lock, spec, nil, false)
	if err != nil {
		return result, err
	}
	if err = e.Docker.Stop(ctx, c, e.owner(record)); err != nil {
		return result, commanderror.New("create_stop_failed", "Environment created, but stopping it failed.", result.Name, err,
			commanderror.Next("Stop", "stop", result.Name))
	}
	return result, nil
}

func (e *Engine) requireNew(ctx context.Context, l *store.Locked) error {
	_, exists, err := e.Docker.Inspect(ctx, l.Name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("container exists without a valid durable contract; refusing adoption")
	}
	dir, err := l.Path(".")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("uncommitted session state exists at %s.\nInspect this directory before retrying creation.", dir)
	}
	return nil
}

func (e *Engine) create(ctx context.Context, l *store.Locked, s environment.Spec, previous *store.Record, force bool) (store.Record, docker.Container, error) {
	return e.createAs(ctx, l, s, previous, force, CreationIdentity{})
}

// CreationIdentity supplies durable identity/activity for a new destination.
// Zero activity means this creation is its first recorded activity.
type CreationIdentity struct {
	ID          string
	Created     time.Time
	Activity    time.Time
	Action      string
	ManualStart bool
}

// CreatePrepared materializes current-format state prepared under the supplied
// operation lock. The caller owns publication and recovery of the prepared
// directory; this method never adopts an existing record or container.
func (e *Engine) CreatePrepared(ctx context.Context, l *store.Locked, s environment.Spec, identity CreationIdentity) (store.Record, docker.Container, error) {
	if !l.Held() || l.Name != s.Identity.Name || identity.Created.IsZero() || len(identity.ID) != 32 {
		return store.Record{}, docker.Container{}, fmt.Errorf("invalid prepared destination identity or lock")
	}
	if _, err := hex.DecodeString(identity.ID); err != nil || strings.ToLower(identity.ID) != identity.ID {
		return store.Record{}, docker.Container{}, fmt.Errorf("invalid prepared session ID")
	}
	if _, err := l.ReadRecord(ctx); err == nil {
		return store.Record{}, docker.Container{}, fmt.Errorf("prepared destination already has a session record")
	} else if !errors.Is(err, os.ErrNotExist) {
		return store.Record{}, docker.Container{}, err
	}
	if _, exists, err := e.Docker.Inspect(ctx, l.Name); err != nil {
		return store.Record{}, docker.Container{}, err
	} else if exists {
		return store.Record{}, docker.Container{}, fmt.Errorf("prepared destination already has a Docker container")
	}
	return e.createAs(ctx, l, s, nil, false, identity)
}

// Transfer and prepared creation supply identity before creating resources.
// Ordinary create/recreate still allocate or preserve their own identity.
func (e *Engine) createAs(ctx context.Context, l *store.Locked, s environment.Spec, previous *store.Record, force bool, seed CreationIdentity) (record store.Record, c docker.Container, err error) {
	id, created := seed.ID, seed.Created
	if err = l.RequireIdle(); err != nil {
		return record, c, err
	}
	if err = e.Docker.Network(ctx, s.Settings.Network); err != nil {
		return record, c, err
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if previous != nil {
		id = previous.ID
		created = previous.Created
	} else if id == "" {
		id, err = fsutil.ID()
		if err != nil {
			return record, c, err
		}
	}
	var image docker.Image
	if previous != nil && !force && previous.Applied.Image == s.Fingerprints.Image {
		available, inspectErr := e.Docker.ImageAvailable(ctx, previous.ImageID)
		if inspectErr != nil {
			return record, c, inspectErr
		}
		if available {
			image, err = e.Docker.InspectImage(ctx, previous.ImageID)
			if err == nil {
				err = image.Verify(e.Store.Installation)
			}
		} else {
			image, err = e.build(ctx, s, id, false)
		}
	} else {
		image, err = e.build(ctx, s, id, force)
	}
	if err != nil {
		return record, c, err
	}
	var old docker.Container
	var existed, removed bool
	if previous != nil {
		old, existed, err = e.inspect(ctx, *previous)
		if err != nil {
			return record, c, err
		}
	}
	// Before removal, a failed sync must restore a previously running original.
	// After removal, the old record and image remain the recovery authority.
	defer func() {
		if err != nil && existed && !removed && old.State.Running {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, e.start(cleanup, old, *previous))
		}
	}()
	if existed && old.State.Running {
		if err = e.Docker.Stop(ctx, old, e.owner(*previous)); err != nil {
			return record, c, err
		}
	}
	mounts, err := e.mountPlan(l, s)
	if err != nil {
		return record, c, err
	}
	if err = e.sync(l, s); err != nil {
		return record, c, err
	}
	if existed {
		if err = e.Docker.Remove(ctx, old, e.owner(*previous)); err != nil {
			return record, c, err
		}
		removed = true
	}
	record = creationRecord(s, image.ID, mounts, id, created, time.Now().UTC(), previous, seed)
	c, err = e.materialize(ctx, record)
	if err != nil {
		return record, c, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			stopErr := e.Docker.Stop(cleanup, c, e.owner(record))
			err = errors.Join(err, stopErr)
			if stopErr == nil {
				err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(record)))
			}
		}
	}()
	record.SetupContainer = c.ID
	if err = l.Save(record); err != nil {
		return record, c, err
	}
	committed = true
	return record, c, nil
}
func (e *Engine) materialize(ctx context.Context, record store.Record) (c docker.Container, err error) {
	// This attempt prepares a new instance; only its successful caller commits
	// the new setup-container ID. Existing-instance access never clears it.
	record.SetupContainer = ""
	if err = prepareMountParents(record); err != nil {
		return c, err
	}
	// Terminal defaults are invocation-local; configured env takes precedence at
	// creation. Recovery uses today's terminal without changing the saved contract.
	plan := record.Creation
	plan.RestartPolicy = restartPolicy(record.ManualStart)
	plan.Env = append(append([]string(nil), e.TerminalEnv...), plan.Env...)
	id, err := e.Docker.Create(ctx, plan, e.owner(record))
	if err != nil {
		return c, err
	}
	c, exists, err := e.inspect(ctx, record)
	if err != nil {
		return c, err
	}
	if !exists || c.ID != id {
		return c, fmt.Errorf("new container identity could not be verified")
	}
	ok := false
	defer func() {
		if !ok {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			stopErr := e.Docker.Stop(cleanup, c, e.owner(record))
			err = errors.Join(err, stopErr)
			if stopErr == nil {
				err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(record)))
			}
		}
	}()
	if err = e.start(ctx, c, record); err != nil {
		return c, err
	}
	c.State.Running = true
	if err = e.installRuntime(ctx, record); err != nil {
		return c, err
	}
	for _, argv := range record.Prepare {
		if err = e.Docker.Exec(ctx, c, e.owner(record), argv, nil, docker.Streams{Out: e.Streams.Out, Err: e.Streams.Err}); err != nil {
			return c, err
		}
	}
	if err = e.runHooks(ctx, c, record, record.Setup); err != nil {
		return c, err
	}
	if err = e.Docker.Exec(ctx, c, e.owner(record), []string{"sh", "-c", `command -v "$1" >/dev/null`, "--", record.Launch.Binary}, nil, docker.Streams{Err: e.Streams.Err}); err != nil {
		return c, err
	}
	ok = true
	return c, nil
}
func (e *Engine) runHooks(ctx context.Context, c docker.Container, r store.Record, hooks []environment.Hook) error {
	for _, hook := range hooks {
		if err := e.Docker.Exec(ctx, c, e.owner(r), []string{"bash", "-s"}, nil, docker.Streams{In: bytes.NewReader(hook.Data), Out: e.Streams.Out, Err: e.Streams.Err}); err != nil {
			return fmt.Errorf("hook %s: %w", hook.Path, err)
		}
	}
	return nil
}

func (e *Engine) Recreate(ctx context.Context, q Request, force bool) (Result, error) {
	// Resolve from the locked record so recreation cannot switch to today's
	// default profile or a source selection read before another mutation.
	target := ""
	if q.Recorded != nil {
		target = q.Recorded.Name
	} else {
		lookup := *e
		lookup.IgnoreProject = lookup.IgnoreProject || q.IgnoreProject
		r, err := lookup.Locate(ctx, q.Workspace, q.Profile)
		if err != nil {
			return Result{}, err
		}
		target = r.Identity.Name
	}
	l, err := e.Store.Lock(ctx, target)
	if err != nil {
		return Result{}, err
	}
	defer l.Close()
	old, err := l.Load()
	if err != nil {
		return Result{}, err
	}
	if q.Recorded != nil && old.Identity != *q.Recorded {
		return Result{}, fmt.Errorf("session source selection changed; retry recreation")
	}
	if err = l.RequireIdle(); err != nil {
		return Result{}, err
	}
	if q.Recorded != nil && q.Profile != "" && q.Profile != old.Identity.Profile {
		return Result{}, fmt.Errorf("profile does not match the recorded session")
	}
	if (e.IgnoreProject || q.IgnoreProject) && old.Identity.Project {
		return Result{}, fmt.Errorf("project exclusion does not match the recorded session")
	}
	q.Workspace, q.Profile, q.Recorded, q.Sources = old.Identity.Workspace, old.Identity.Profile, &old.Identity, old.Sources
	s, err := e.Resolve(q)
	if err != nil {
		return Result{}, err
	}
	container, exists, err := e.inspect(ctx, old)
	if err != nil {
		return Result{}, err
	}
	running := old.ManualStart || (exists && container.State.Running)
	r, c, err := e.create(ctx, l, s, &old, force)
	if err != nil {
		return Result{}, err
	}
	if !running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return Result{}, err
		}
	}
	return Result{Name: s.Identity.Name}, nil
}
