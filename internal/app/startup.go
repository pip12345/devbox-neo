package app

import (
	"context"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func (e *Engine) creationDrift(result *Result, r store.Record, spec environment.Spec) {
	drift := environment.CompareInputs(r.Inputs, spec.Inputs)
	if drift.Change == environment.Recreate || drift.Change == environment.RebuildAndRecreate {
		e.diagnose(result, Diagnostic{Code: "creation_drift", Message: "this container differs from current configuration:", Command: []string{"devbox-neo", "recreate", r.Identity.Name}, Change: drift.Change, PendingInputChanges: drift.PendingCreationChanges()})
	}
}

// All ordinary access commands synchronize at the stopped-to-running boundary.
// Open supplies its resolved invocation; other access commands resolve only if
// startup is needed. Transaction rollback uses recorded startup, not this path.
func (e *Engine) startAccess(ctx context.Context, lock *store.Locked, c docker.Container, exists bool, r *store.Record, desired *environment.Spec, result *Result) (docker.Container, bool, error) {
	if exists && c.State.Running {
		return c, false, e.syncRestart(ctx, c, *r)
	}
	if err := lock.RequireIdle(); err != nil {
		return c, false, err
	}
	if desired == nil {
		spec, err := e.resolveSpec(Request{Workspace: r.Identity.Workspace, Profile: r.Identity.Profile, Recorded: &r.Identity, Sources: r.Sources})
		if err != nil {
			e.resolutionWarnings(spec)
			return c, false, err
		}
		e.creationDrift(result, *r, spec)
		e.resolutionWarnings(spec)
		desired = &spec
	}
	if !exists {
		c, err := e.recover(ctx, lock, r, desired)
		return c, err == nil, err
	}
	if err := e.syncRecordedConfig(lock, r, *desired); err != nil {
		return c, false, err
	}
	if err := e.start(ctx, c, *r); err != nil {
		return c, false, err
	}
	c.State.Running = true
	return c, true, nil
}

func applyLaunch(r *store.Record, spec environment.Spec) {
	if r.Definition.Hash == spec.Harness.Hash {
		r.Launch.Args = append(append([]string(nil), spec.Harness.Definition.Launch.Args...), spec.Settings.HarnessArgs...)
	}
	r.Launch.Shell = append([]string(nil), spec.Settings.Shell...)
}

func (e *Engine) syncRecordedConfig(lock *store.Locked, r *store.Record, desired environment.Spec) error {
	// A changed harness definition may describe different mounts or ownership.
	// Only recreation can adopt that contract; ordinary access keeps the old one.
	if r.Definition.Hash == desired.Harness.Hash {
		// Validate durable backing roots before sync can create subdirectories.
		if err := prepareMountParents(*r); err != nil {
			return err
		}
		if err := e.sync(lock, desired); err != nil {
			return err
		}
		r.ApplyRuntime(desired.Inputs.Runtime)
	}
	applyLaunch(r, desired)
	return nil
}
