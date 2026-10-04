package app

import (
	"context"
	"errors"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

type recreatePlan struct {
	lock                    *store.Locked
	record                  store.Record
	spec                    environment.Spec
	container               docker.Container
	change                  environment.Change
	replace, image, running bool
}

// Planning is read-only under the session lock. Bulk application resolves all
// selected configs before starting; later Docker failures can still be partial.
func (e *Engine) planRecreate(ctx context.Context, lock *store.Locked, r store.Record, q Request, image bool) (recreatePlan, error) {
	p := recreatePlan{lock: lock, record: r, image: image}
	if err := lock.RequireIdle(); err != nil {
		return p, err
	}
	if err := checkDurableStores(lock, r); err != nil {
		return p, err
	}
	q.Workspace, q.LocalName, q.SessionID, q.Sources = r.Settings.Workspace, r.Settings.LocalName, r.ID, r.Settings.Sources
	var err error
	p.spec, err = e.Resolve(q)
	if err != nil {
		return p, err
	}
	var exists bool
	p.container, exists, err = e.inspect(ctx, r)
	if err != nil {
		return p, err
	}
	p.change = environment.CompareInputs(r.Applied.Inputs, p.spec.Inputs).Change
	p.replace = !exists || image || q.ForceContainer || p.change == environment.Recreate || p.change == environment.RebuildAndRecreate
	p.running = r.Settings.ManualStart || (exists && p.container.State.Running)
	return p, nil
}

func (e *Engine) applyRecreate(ctx context.Context, p recreatePlan) (result Result, err error) {
	result.SessionID = p.record.ID
	message := "Apply runtime configuration without replacing the container. It may be briefly started or restarted."
	if p.replace {
		message = "Replace the container with current configuration; reuse a compatible available image or build one. Container-local changes will be lost."
		if p.image {
			message = "Build the image without cache and replace the container. Container-local changes will be lost."
		} else if p.change == environment.RebuildAndRecreate {
			message = "Build changed image inputs and replace the container. Container-local changes will be lost."
		}
	}
	e.diagnose(&result, Diagnostic{Code: "apply_plan", Message: message, Target: p.record.ID})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !p.replace {
		return result, e.applyRuntime(ctx, p)
	}
	r, c, err := e.create(ctx, p.lock, p.spec, &p.record, p.image)
	if err != nil {
		return result, err
	}
	if !p.running {
		if err := e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return result, commanderror.New("apply_stop_failed", "Configuration applied, but stopping the container failed.", r.ID, err, commanderror.Next("Stop", "stop", r.ID))
		}
	}
	return result, nil
}

func (e *Engine) applyRuntime(ctx context.Context, p recreatePlan) (err error) {
	r, c := p.record, p.container
	if err := checkDurableStores(p.lock, r); err != nil {
		return err
	}
	applied := false
	// Files can be partially reconciled on an I/O failure; do not pretend to
	// roll them back. Preserve runtime lifetime independently of command cancellation.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		live, exists, restoreErr := e.inspect(cleanup, r)
		if restoreErr == nil && !exists {
			restoreErr = commanderror.New("container_missing", "Container disappeared during configuration application.", r.ID, nil)
		}
		if restoreErr == nil && exists {
			if p.running && !live.State.Running {
				restoreErr = e.start(cleanup, live, r)
			}
			if !p.running && live.State.Running {
				restoreErr = e.Docker.Stop(cleanup, live, e.owner(r))
			}
		}
		err = errors.Join(err, restoreErr)
		if err != nil {
			message := "Runtime configuration application failed; some managed files may have changed. Repair the cause and retry."
			next := commanderror.Next("Apply current configuration", "recreate", r.ID)
			if applied {
				message = "Configuration applied, but restoring the container's running/stopped state failed."
				if exists {
					next = commanderror.Next("Restore stopped state", "stop", r.ID)
					if p.running {
						next = commanderror.Next("Restore running state", "start", r.ID)
					}
				}
			}
			err = commanderror.New("runtime_apply_failed", message, r.ID, err,
				commanderror.Next("Inspect session", "status", r.ID), next)
		}
	}()
	if c.State.Running {
		if err = e.Docker.Stop(ctx, c, e.owner(r)); err != nil {
			return err
		}
	}
	if err = e.sync(p.lock, p.spec); err != nil {
		return err
	}
	if err = e.start(ctx, c, r); err != nil {
		return err
	}
	c.State.Running = true
	if err = e.installRuntime(ctx, r); err != nil {
		return err
	}
	if err = e.installOpenHooks(ctx, c, r, p.spec.BeforeOpen); err != nil {
		return err
	}
	d := p.spec.Harness.Definition
	r.Applied.Launch = store.Launch{Binary: d.Binary, Args: append(append([]string(nil), d.Launch.Args...), p.spec.Settings.HarnessArgs...), Continue: d.Launch.Continue, Shell: p.spec.Settings.Shell}
	r.ApplyRuntime(p.spec.Inputs.Runtime)
	r.Applied.Inputs.Sources = p.spec.ResolvedSources
	r.Action, r.Activity = "recreate", time.Now().UTC()
	if err = p.lock.Save(r); err != nil {
		return err
	}
	applied = true
	return nil
}
