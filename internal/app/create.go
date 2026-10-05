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
func (e *Engine) Create(ctx context.Context, q CreateRequest) (CreationResult, error) {
	identity, err := environment.Identify(q.Workspace, q.LocalName)
	if err != nil {
		return CreationResult{}, err
	}
	q.Workspace = identity.Workspace
	namesLock, err := e.Store.LockNames(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	defer fsutil.Unlock(namesLock)
	if err := e.Store.RequireUnusedBinding(ctx, identity.Binding, ""); err != nil {
		return CreationResult{}, err
	}
	directory, err := store.AllocateDirectory(identity.Binding)
	if err != nil {
		return CreationResult{}, err
	}
	id, err := fsutil.ID()
	if err != nil {
		return CreationResult{}, err
	}
	result := CreationResult{Result: Result{Session: directory, SessionID: id}}
	lock, err := e.Store.Lock(ctx, directory, id)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if _, err = lock.Load(); err == nil {
		return result, commanderror.New("session_exists", "Environment already exists.", directory, nil,
			commanderror.Next("Open", "open", directory),
			commanderror.Next("Or recreate with current configuration", "recreate", directory))
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	spec, err := e.Resolve(ResolveRequest{Workspace: q.Workspace, LocalName: q.LocalName, Sources: q.Sources, Host: q.Host})
	e.reportWarnings(spec.Warnings)
	if err != nil {
		return result, err
	}
	if err = e.requireNew(ctx, lock); err != nil {
		return result, err
	}
	if q.MakeDefault {
		if _, err := e.Store.ReadDefault(ctx, identity.Workspace); err != nil {
			return result, err
		}
	}
	record, c, err := e.createAs(ctx, lock, spec, nil, creationOptions{}, CreationIdentity{ID: id})
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return result, errors.Join(err, e.discardUncommittedCreation(cleanup, lock, id))
	}
	result.Saved = true
	var stopErr, defaultErr error
	if err = e.Docker.Stop(ctx, c, e.owner(record)); err != nil {
		stopErr = commanderror.New("create_stop_failed", "Environment created, but stopping it failed.", record.Directory, err,
			commanderror.Next("Stop", "stop", record.Directory))
	}
	// The session is committed. A failed preference update must not discard it
	// or encourage retrying Create; default selection has its own repair step.
	if q.MakeDefault {
		if err := lock.SelectDefault(record.ID); err != nil {
			defaultErr = commanderror.New("create_default_failed", "Session created, but selecting it as the folder default failed.", record.Directory, err,
				commanderror.Next("Select the created session as default", "edit", record.Directory, "--default"))
		}
	}
	return result, errors.Join(stopErr, defaultErr)
}

// A failed new allocation has no prior session history. Retain it if a record
// was published or any container still uses its ID; otherwise retries must not
// accumulate anonymous state directories after preparation failures.
func (e *Engine) discardUncommittedCreation(ctx context.Context, lock *store.Locked, id string) error {
	root, err := lock.Path(".")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := lock.ReadRecord(ctx); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	live, err := e.Docker.Inventory(ctx, e.Store.Installation)
	if err != nil {
		return err
	}
	for _, c := range live {
		if c.Config.Labels[docker.Namespace+".session"] == id {
			return fmt.Errorf("uncommitted state retained at %s because its container still exists", root)
		}
	}
	return lock.DeleteContext(ctx)
}

func (e *Engine) requireNew(ctx context.Context, l *store.Locked) error {
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

type creationOptions struct {
	image bool
	force bool
}

// CreationIdentity supplies identity and creation time for a new destination.
// Transfers pin these inputs before preparing the destination's state.
type CreationIdentity struct {
	ContainerName string
	ID            string
	Created       time.Time
	ManualStart   bool
}

// CreatePrepared materializes current-format state prepared under the supplied
// operation lock. The caller owns publication and recovery of the prepared
// directory; this method never adopts an existing record or container.
func (e *Engine) CreatePrepared(ctx context.Context, l *store.Locked, s environment.Spec, identity CreationIdentity) (store.Record, docker.Container, error) {
	if !l.Held() || l.ID != identity.ID || identity.Created.IsZero() || len(identity.ID) != 32 {
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
	return e.createAs(ctx, l, s, nil, creationOptions{}, identity)
}

// Session identity is allocated before locking. Materialization may replace
// runtime resources, but cannot change the identity protected by that lock.
func (e *Engine) createAs(ctx context.Context, l *store.Locked, s environment.Spec, previous *store.Record, options creationOptions, seed CreationIdentity) (record store.Record, c docker.Container, err error) {
	id, created := l.ID, seed.Created
	if (previous == nil && seed.ID != "" && seed.ID != id) || (previous != nil && previous.ID != id) {
		return record, c, fmt.Errorf("creation identity differs from its operation lock")
	}
	var attachments []store.Lease
	if options.force {
		if previous == nil {
			return record, c, fmt.Errorf("forced replacement requires an existing session")
		}
		attachments, err = l.LiveLeases()
	} else {
		err = l.RequireIdle()
	}
	if err != nil {
		return record, c, err
	}
	if previous != nil {
		if err = checkDurableStores(l, *previous); err != nil {
			return record, c, err
		}
		for _, mount := range s.ExtraMounts {
			if mount.Kind != "volume" {
				continue
			}
			for _, old := range previous.Applied.Creation.Mounts {
				if old.Kind == "volume" && old.Source == mount.Source {
					if err = e.Docker.Volume(ctx, mount.Source); err != nil {
						return record, c, err
					}
				}
			}
		}
	}
	if err = e.Docker.CheckRawVolumes(ctx, s.Settings.DockerArgs); err != nil {
		return record, c, err
	}
	if err = e.Docker.Network(ctx, s.Settings.Network); err != nil {
		return record, c, err
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if previous != nil {
		created = previous.Created
	}
	s.Identity.Name = seed.ContainerName
	if s.Identity.Name == "" {
		allocation, allocationErr := fsutil.ID()
		if allocationErr != nil {
			return record, c, allocationErr
		}
		s.Identity.Name = environment.ResourceName(s.Identity.Workspace, s.Identity.LocalName, allocation)
	}
	if _, exists, err := e.Docker.Inspect(ctx, s.Identity.Name); err != nil {
		return record, c, err
	} else if exists {
		return record, c, fmt.Errorf("allocated container name is occupied")
	}
	var image docker.Image
	reused := false
	if previous != nil && !options.image && previous.Applied.Fingerprints.Image == s.Fingerprints.Image {
		available, inspectErr := e.Docker.ImageAvailable(ctx, previous.Applied.ImageID)
		if inspectErr != nil {
			return record, c, inspectErr
		}
		if available {
			image, err = e.Docker.InspectImage(ctx, previous.Applied.ImageID)
			if err != nil {
				return record, c, err
			}
			reused = image.Verify(e.Store.Installation) == nil
		}
		// Cache eligibility does not authorize adoption. A missing or unowned
		// image is replaced by a fresh owned build, never executed or mutated.
		if !reused {
			image, err = e.build(ctx, s, id, false)
		}
	} else {
		image, err = e.build(ctx, s, id, options.image)
	}
	if err != nil {
		return record, c, err
	}
	if !reused {
		// An uncommitted build tag must not point a surviving record at the
		// wrong image after failure. Absence is valid: runtime is disposable.
		tag := environment.ImageTag(s.Identity.Workspace, s.Identity.LocalName, id)
		defer func() {
			if err == nil {
				return
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, tagged, inspectErr := e.Docker.TaggedImage(cleanup, tag)
			err = errors.Join(err, inspectErr)
			if inspectErr == nil && tagged {
				err = errors.Join(err, e.Docker.Untag(cleanup, tag, image.ID, e.Store.Installation))
			}
		}()
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
	// After removal, retain the saved identity and stores for explicit retry;
	// the next attempt resolves current inputs rather than restoring old runtime.
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
	// Keep old attachments registered until the verified runtime is gone.
	// Late cleanup sees a missing lease and cannot affect the replacement.
	for _, lease := range attachments {
		if _, err = l.Release(lease.ID); err != nil {
			return record, c, err
		}
	}
	record = creationRecord(s, image.ID, mounts, id, created, time.Now().UTC(), previous, seed)
	if reused {
		record.Applied.ImageTag = previous.Applied.ImageTag
	}
	record.Directory = l.Name
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
	record.Applied.SetupContainer = c.ID
	if err = l.Save(record); err != nil {
		return record, c, err
	}
	committed = true
	return record, c, nil
}
func (e *Engine) materialize(ctx context.Context, record store.Record) (c docker.Container, err error) {
	// This attempt prepares a new instance; only its successful caller commits
	// the new setup-container ID. Existing-instance access never clears it.
	record.Applied.SetupContainer = ""
	if err = prepareMountParents(record); err != nil {
		return c, err
	}
	// Terminal defaults are invocation-local; configured env takes precedence at
	// creation. Terminal values never enter the saved comparison baseline.
	plan := record.Applied.Creation
	plan.RestartPolicy = restartPolicy(record.Settings.ManualStart)
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
	if err = e.installOpenHooks(ctx, c, record, record.Applied.BeforeOpen); err != nil {
		return c, err
	}
	for _, argv := range record.Applied.Prepare {
		if err = e.Docker.Exec(ctx, c, e.owner(record), argv, nil, docker.Streams{Out: e.Streams.Out, Err: e.Streams.Err}); err != nil {
			return c, err
		}
	}
	if err = e.runHooks(ctx, c, record, record.Applied.Setup); err != nil {
		return c, err
	}
	if err = e.Docker.Exec(ctx, c, e.owner(record), []string{"sh", "-c", `command -v "$1" >/dev/null`, "--", record.Applied.Launch.Binary}, nil, docker.Streams{Err: e.Streams.Err}); err != nil {
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

func (e *Engine) Recreate(ctx context.Context, q RecreateRequest, image bool) (Result, error) {
	// Resolve from the locked record, never a source list captured by the CLI
	// before another mutation or a default changed after this invocation chose it.
	selected, err := e.Locate(ctx, q.Target, q.LocalName)
	if err != nil {
		return Result{}, err
	}
	l, err := e.Store.Lock(ctx, selected.Directory, selected.ID)
	if err != nil {
		return Result{}, err
	}
	defer l.Close()
	old, err := loadSelected(l, selected)
	if err != nil {
		return Result{}, err
	}
	plan, err := e.planRecreate(ctx, l, old, q.Options, image)
	e.reportWarnings(plan.spec.Warnings)
	if err != nil {
		return Result{}, err
	}
	return e.applyRecreate(ctx, plan)
}
