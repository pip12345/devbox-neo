package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

// Requirement supplies the blocking UI's explanation and approved operation.
// Version-specific conversion and effects remain in this package, not the UI.
type Requirement struct {
	Name        string
	Description string
	Warning     string
	apply       func(context.Context, docker.Runtime) error
}

func (r Requirement) Apply(ctx context.Context, runtime docker.Runtime) error {
	return r.apply(ctx, runtime)
}

// Pending recognizes formats, not runnable sessions. It uses no Docker or
// configuration resolution. Ordinary readers still diagnose broken records and
// incomplete allocations when no known migration is required.
func Pending(ctx context.Context, home string) (*Requirement, error) {
	runtimeUpdate, err := runtimeMigration(ctx, home)
	if err != nil {
		return nil, err
	}
	defaults, err := defaultsPending(home)
	if err != nil {
		return nil, err
	}
	if !defaults {
		return runtimeUpdate, nil
	}
	required := &Requirement{
		Name:        "Consolidate folder defaults",
		Description: "Saved folder defaults move into one file. Existing selections are kept, including references to unavailable sessions. Empty selections are omitted.\nContainers, session data and history are unchanged.",
		Warning:     "Close other dbx commands before migrating. Do not use older builds with this home afterward.",
		apply:       func(ctx context.Context, _ docker.Runtime) error { return migrateFolderDefaults(ctx, home) },
	}
	if runtimeUpdate != nil {
		required.Name = runtimeUpdate.Name + " and folder defaults"
		required.Description = runtimeUpdate.Description + "\nSaved folder defaults also move into one file, preserving existing selections."
		required.Warning = runtimeUpdate.Warning + " " + required.Warning
		required.apply = func(ctx context.Context, runtime docker.Runtime) error {
			if err := runtimeUpdate.Apply(ctx, runtime); err != nil {
				return err
			}
			return migrateFolderDefaults(ctx, home)
		}
	}
	return required, nil
}

func runtimeMigration(ctx context.Context, home string) (*Requirement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := fsutil.Path(home, "sessions")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	needed := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		path, err := fsutil.Path(root, filepath.Join(entry.Name(), "session.json"))
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var header struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(data, &header) != nil || header.Version == 0 {
			continue
		}
		switch header.Version {
		case 6:
			needed = true
		case store.RecordVersion:
		default:
			return nil, commanderror.New("migration_unavailable", fmt.Sprintf("Saved state uses unsupported schema %d; no migration is available in this build.", header.Version), home, nil)
		}
	}
	if !needed {
		return nil, nil
	}
	return &Requirement{
		Name:        "Update Docker naming and saved session records",
		Description: "This update applies to all saved sessions in this Devbox home.\nSaved harness history, configs, auth, caches and workspace defaults are kept.\nOld linked containers and image tags are removed. Use Recreate afterward to rebuild runtime from current config.",
		Warning:     "Container-local files and tools will be lost. Finish active commands and pending transfers with the previous build first.",
		apply: func(ctx context.Context, runtime docker.Runtime) error {
			_, err := Run(ctx, home, runtime, true)
			return err
		},
	}, nil
}
