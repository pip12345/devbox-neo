package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"devbox/internal/store"
)

// ReferencingSessions is advisory context. It does not lock sessions for the
// duration of a directory edit or require their sources to resolve successfully.
func (s Service) ReferencingSessions(ctx context.Context, owner Owner) ([]string, error) {
	entries, err := (&store.Store{Home: s.Home}).Inventory(ctx)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	var issues error
	for _, entry := range entries {
		if entry.Err != nil {
			issues = errors.Join(issues, entry.Err)
			continue
		}
		for _, reference := range entry.Record.Sources {
			source, err := reference.Expand(entry.Record.Identity.Workspace)
			if err != nil {
				issues = errors.Join(issues, err)
				continue
			}
			path := source.Path
			if canonical, err := filepath.EvalSymlinks(path); err == nil {
				path = canonical
			}
			if path == owner.Root {
				names = append(names, entry.Name)
				break
			}
		}
	}
	sort.Strings(names)
	return names, issues
}
