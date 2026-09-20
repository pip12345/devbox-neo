package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/config"
	"devbox/internal/environment"
)

// ProjectDirectory locates saved overrides for the requested workspace/profile,
// not an arbitrary available session. Multiple bindings require an exact target;
// profile-only selection never consults project bindings.
func (s *Store) ProjectDirectory(ctx context.Context, workspace, profile string, ignore bool) (string, error) {
	profile, ignore, err := config.SelectionDefaults(s.Home, profile, ignore, config.Snapshot())
	if err != nil {
		return "", err
	}
	if ignore {
		return "", nil
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(filepath.Join(s.Home, "sessions"))
	if err != nil {
		return "", err
	}
	// Only these slots can supply a project binding for this selection. Names
	// narrow the lookup; Read still validates the complete saved contract.
	projectName := environment.ContainerName(absolute, environment.Slot("", true))
	combinedName := environment.ContainerName(absolute, environment.Slot(profile, true))
	selected := ""
	for _, entry := range entries {
		if entry.Name() != projectName && entry.Name() != combinedName {
			continue
		}
		r, readErr := s.Read(ctx, entry.Name())
		if readErr != nil {
			return "", fmt.Errorf("cannot select a saved project directory while session %s is unreadable; use an exact session target: %w", entry.Name(), readErr)
		}
		id := r.Identity
		if id.Workspace != absolute || id.ProjectDir == "" || !id.Project || (id.Profile != "" && id.Profile != profile) {
			continue
		}
		if selected != "" && selected != id.ProjectDir {
			return "", fmt.Errorf("multiple project-directory overrides match this folder; use an exact session target")
		}
		selected = id.ProjectDir
	}
	return selected, nil
}
