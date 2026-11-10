package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/store"
)

// Files without a record have no session identity. Only an explicit directory
// selector or its captured preview can authorize this namespace-locked cleanup.
func (e *Engine) incompleteDeletionTargets(ctx context.Context, options DeleteOptions) (selectedTargets, Selection, error) {
	remaining := options.Selection
	remaining.Targets = slices.Clone(remaining.Targets)
	remaining.Captured = nil
	remaining.IncompleteDirs = nil
	if len(options.Selection.IncompleteDirs) > 0 && (remaining.All || remaining.Stopped || options.Orphaned || options.OlderThan > 0 || remaining.LocalName != "") {
		return nil, remaining, fmt.Errorf("--incomplete-dir cannot combine with session filters or --name")
	}
	incomplete := map[string]SelectedTarget{}
	for _, name := range options.Selection.IncompleteDirs {
		directory, err := e.Store.InspectIncompleteDirectory(ctx, name)
		if err != nil {
			incomplete[name] = SelectedTarget{name: name, err: err}
			continue
		}
		if directory == nil {
			path, pathErr := e.Store.RecordPath(name)
			if pathErr != nil {
				incomplete[name] = SelectedTarget{name: name, err: pathErr}
			} else if _, statErr := os.Lstat(path); statErr == nil || !os.IsNotExist(statErr) {
				incomplete[name] = SelectedTarget{name: name, err: fmt.Errorf("directory contains saved session state; --incomplete-dir cannot remove it: %s", name)}
			}
			continue
		}
		incomplete[name] = SelectedTarget{name: name, incomplete: directory}
	}
	for _, target := range options.Selection.Captured {
		if target.incomplete == nil {
			remaining.Captured = append(remaining.Captured, target)
			continue
		}
		if target.sessionID != "" || target.incomplete.Path != filepath.Join(e.Store.Home, "sessions", target.name) {
			target.err = fmt.Errorf("invalid captured incomplete directory")
		}
		incomplete[target.name] = target
	}
	return orderedTargets(incomplete), remaining, nil
}

func (e *Engine) checkIncompleteDeletion(ctx context.Context, directory *store.IncompleteDirectory) error {
	if err := directory.Check(ctx); err != nil {
		return err
	}
	containers, err := e.Docker.AllContainers(ctx)
	if err != nil {
		return err
	}
	for _, container := range containers {
		name := strings.TrimPrefix(container.Name, "/")
		if name == directory.Name {
			return fmt.Errorf("target names both an incomplete directory and a container; inspect before cleanup: %s", directory.Name)
		}
		for _, mount := range container.Mounts {
			if mount.Type != "bind" {
				continue
			}
			if !filepath.IsAbs(mount.Source) {
				return fmt.Errorf("invalid container %s bind source", name)
			}
			// A bind through an external symlink still keeps these files in use.
			source, err := config.CanonicalPath(mount.Source)
			if err != nil {
				return fmt.Errorf("cannot verify container %s bind source %s: %w", name, mount.Source, err)
			}
			if containsPath(directory.Path, source) || containsPath(source, directory.Path) {
				return commanderror.New("incomplete_directory_in_use", "Cannot delete incomplete creation files: a container still mounts this directory.", directory.Name, fmt.Errorf("container %s uses %s", name, mount.Source))
			}
		}
	}
	return nil
}

func containsPath(root, path string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}
