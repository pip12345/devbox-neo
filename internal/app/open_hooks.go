package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// Hook bytes belong to the disposable container, not the saved session record.
// Content-addressed names let a failed apply leave the previous launch contract
// usable: new hooks do not overwrite the scripts referenced by the old record.
func (e *Engine) installOpenHooks(ctx context.Context, c docker.Container, r store.Record, hooks []environment.Hook) error {
	if len(hooks) == 0 {
		return nil
	}
	dir, err := os.MkdirTemp(e.Store.Home, ".hooks-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, hook := range hooks {
		if environment.Digest(hook.Data) != hook.Hash {
			return fmt.Errorf("captured hook content changed")
		}
		if err := fsutil.Write(filepath.Join(dir, hook.Hash+".sh"), hook.Data, 0600); err != nil {
			return err
		}
	}
	return e.Docker.InstallOpenHooks(ctx, c, e.owner(r), dir)
}

func (e *Engine) runAppliedOpenHooks(ctx context.Context, c docker.Container, r store.Record) error {
	var paths []string
	for _, hook := range r.Applied.Inputs.Runtime.BeforeOpen {
		paths = append(paths, docker.OpenHookPath(hook.Hash))
	}
	if len(paths) == 0 {
		return nil
	}
	// Check the entire chain before executing any hook. Older containers have no
	// installed copies; only an explicit apply may read current source scripts.
	if err := e.Docker.CheckOpenHooks(ctx, c, e.owner(r), paths); err != nil {
		return commanderror.New("applied_hooks_unavailable", "Applied before-open scripts are unavailable. Apply current config explicitly; Shell and Exec remain available for repair.", r.Directory, err,
			commanderror.Next("Install current before-open scripts", "recreate", r.Directory),
			commanderror.Next("Or open a repair shell", "shell", r.Directory))
	}
	for i, path := range paths {
		if err := e.Docker.Exec(ctx, c, e.owner(r), []string{"bash", path}, nil, docker.Streams{Out: e.Streams.Out, Err: e.Streams.Err}); err != nil {
			return commanderror.New("before_open_failed", "Applied before-open script failed: "+r.Applied.Inputs.Runtime.BeforeOpen[i].Source, r.Directory, err,
				commanderror.Next("Open a repair shell", "shell", r.Directory),
				commanderror.Next("Apply corrected configuration", "recreate", r.Directory))
		}
	}
	return nil
}
