package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// Incomplete directories are selectable only by their exact inventory name.
// There is no committed identity or activity with which to apply bulk filters.
func (e *Engine) incompleteDeletionTargets(ctx context.Context, options DeleteOptions) ([]*store.IncompleteDirectory, []string, error) {
	remaining := slices.Clone(options.Selection.Targets)
	if options.Selection.All || options.Selection.Stopped || options.Orphaned || options.OlderThan > 0 || options.Selection.LocalName != "" {
		return nil, remaining, nil
	}
	incomplete := []*store.IncompleteDirectory{}
	remaining = nil
	seen := map[string]bool{}
	for _, target := range options.Selection.Targets {
		if !environment.ValidResourceName(target) {
			remaining = append(remaining, target)
			continue
		}
		directory, err := e.Store.InspectIncompleteDirectory(ctx, target)
		if err != nil {
			return nil, nil, err
		}
		if directory == nil {
			remaining = append(remaining, target)
			continue
		}
		if !seen[target] {
			incomplete = append(incomplete, directory)
			seen[target] = true
		}
	}
	return incomplete, remaining, nil
}

func (e *Engine) checkIncompleteDeletion(ctx context.Context, directories []*store.IncompleteDirectory) error {
	if len(directories) == 0 {
		return nil
	}
	for _, directory := range directories {
		if err := directory.Check(ctx); err != nil {
			return err
		}
	}
	containers, err := e.Docker.AllContainers(ctx)
	if err != nil {
		return err
	}
	for _, directory := range directories {
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
				// External containers may bind through a symlink outside the
				// home. Compare canonical host paths, not just mount spelling.
				source, err := config.CanonicalPath(mount.Source)
				if err != nil {
					return fmt.Errorf("cannot verify container %s bind source %s: %w", name, mount.Source, err)
				}
				if containsPath(directory.Path, source) || containsPath(source, directory.Path) {
					return commanderror.New("incomplete_directory_in_use", "Cannot delete incomplete creation files: a container still mounts this directory.", directory.Name, fmt.Errorf("container %s uses %s", name, mount.Source))
				}
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

func (e *Engine) removeIncompleteDirectories(ctx context.Context, directories []*store.IncompleteDirectory, dryRun bool) ([]string, error) {
	removed := []string{}
	for _, directory := range directories {
		if !dryRun {
			if err := directory.Remove(ctx); err != nil {
				return removed, err
			}
		}
		removed = append(removed, directory.Name)
	}
	return removed, nil
}
