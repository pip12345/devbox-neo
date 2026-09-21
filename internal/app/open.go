package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/filesync"
	"devbox/internal/store"
)

func creationRequired(workspace, profile string, cause error) error {
	message := "No environment exists."
	if profile != "" {
		message = fmt.Sprintf("No environment exists (profile: %s).", profile)
	}
	return commanderror.New("session_missing", message, workspace, cause,
		commanderror.Next("Create", "create", workspace))
}

// Open keeps the operation lock through stopped-only synchronization, startup,
// and lease creation. The long foreground command runs after releasing it.
func (e *Engine) Open(ctx context.Context, q Request) (result Result, err error) {
	invocationArgs := append([]string(nil), q.Overrides.HarnessArgs...)
	q.Overrides = config.Layer{}
	lookup := *e
	lookup.IgnoreProject = e.IgnoreProject || q.IgnoreProject
	r, loadErr := lookup.Locate(ctx, q.Workspace, q.Profile)
	if loadErr != nil {
		var missing *commanderror.Error
		if errors.As(loadErr, &missing) && missing.Code == "session_missing" && !strings.HasPrefix(q.Workspace, environment.ContainerPrefix) {
			spec, resolveErr := e.resolveSpec(q)
			if resolveErr != nil {
				return result, resolveErr
			}
			return result, creationRequired(q.Workspace, spec.Identity.Profile, loadErr)
		}
		return result, loadErr
	}
	q.Workspace, q.Profile, q.Recorded, q.Sources = r.Identity.Workspace, r.Identity.Profile, &r.Identity, r.Sources
	// Defer resolution warnings so creation drift is visible before any other
	// open output, especially before entrypoint or harness output can scroll it away.
	spec, err := e.resolveSpec(q)
	if err != nil {
		e.resolutionWarnings(spec)
		return result, err
	}
	result.Name = spec.Identity.Name
	if _, err = store.ProcessIdentity(os.Getpid()); err != nil {
		return result, err
	}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	record, err := lock.Load()
	if errors.Is(err, os.ErrNotExist) {
		return result, creationRequired(q.Workspace, spec.Identity.Profile, err)
	}
	if err != nil {
		return result, err
	}
	e.creationDrift(&result, record, spec)
	e.resolutionWarnings(spec)
	var c docker.Container
	started := false
	defer func() {
		if err != nil && started && lock.Held() {
			err = errors.Join(err, e.stopUnattached(lock, record))
		}
	}()
	var exists bool
	c, exists, err = e.inspect(ctx, record)
	if err != nil {
		return result, err
	}
	wasRunning := exists && c.State.Running
	c, started, err = e.startAccess(ctx, lock, c, exists, &record, &spec, &result)
	if err == nil && wasRunning {
		compatible := record.Definition.Hash == spec.Harness.Hash
		if compatible && record.Applied.Runtime != spec.Fingerprints.Runtime {
			manifest, pathErr := lock.Path(filepath.Join("harnesses", record.Definition.Name, "managed-config.json"))
			if pathErr != nil {
				return result, pathErr
			}
			current, checkErr := filesync.Current(manifest, spec.Files, spec.Harness.Definition.Merge)
			if checkErr != nil {
				return result, checkErr
			}
			if current {
				record.ApplyRuntime(spec.Inputs.Runtime)
			} else {
				e.diagnose(&result, Diagnostic{Code: "runtime_deferred", Message: "managed configuration is deferred while running; it will apply at the next startup", Command: []string{"devbox-neo", "stop", record.Identity.Name}})
			}
		}
		applyLaunch(&record, spec)
	}
	if err != nil {
		return result, err
	}
	if err = e.installRuntime(ctx, record); err != nil {
		return result, err
	}
	if err = e.runHooks(ctx, c, record, spec.BeforeOpen); err != nil {
		return result, err
	}
	record.Activity = time.Now().UTC()
	record.Action = "open"
	if err = lock.Save(record); err != nil {
		return result, err
	}
	argv := append([]string{record.Launch.Binary}, record.Launch.Args...)
	if q.Continue {
		argv = append(argv, record.Launch.Continue...)
	}
	argv = append(argv, invocationArgs...)
	argv = append(argv, q.Args...)
	err = e.attach(ctx, lock, c, record, "open", argv)
	return result, err
}
