package resource

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"devbox/internal/config"
	"devbox/internal/store"
)

// ConfigUse keeps desired references separate from the directories committed
// when a session was built. Removing a desired reference does not release the
// committed source until the session is recreated or removed.
type ConfigUse struct {
	Session   string
	Desired   bool
	Committed bool
}

// ConfigUsers includes sources at or beneath the directory, since deletion
// removes its entire tree. It is advisory for editing and a current-state guard
// for deletion. Known users survive errors from corrupt records, pending
// transfers, or unresolved aliases that make the report incomplete.
func (s Service) ConfigUsers(ctx context.Context, owner Owner) ([]ConfigUse, error) {
	entries, err := (&store.Store{Home: s.Home}).Inventory(ctx)
	if err != nil {
		return nil, err
	}
	users := []ConfigUse{}
	var issues error
	for _, entry := range entries {
		if entry.Err != nil {
			issues = errors.Join(issues, fmt.Errorf("session %s: %w", entry.Name, entry.Err))
			continue
		}
		if entry.Pending != nil {
			issues = errors.Join(issues, fmt.Errorf("session %s has a pending transfer", entry.Name))
		}
		if entry.Record.ID == "" {
			issues = errors.Join(issues, fmt.Errorf("session %s has no readable record", entry.Name))
			continue
		}
		user := ConfigUse{Session: entry.Record.ID}
		for _, reference := range entry.Record.Settings.Sources {
			source, err := reference.Expand(entry.Record.Settings.Workspace)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s source: %w", entry.Name, err))
				continue
			}
			used, err := usesConfigDirectory(owner.Root, source.Path)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s source: %w", entry.Name, err))
				continue
			}
			user.Desired = user.Desired || used
		}
		for _, source := range entry.Record.Applied.Inputs.Sources {
			used, err := usesConfigDirectory(owner.Root, source.Path)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s config source: %w", entry.Name, err))
				continue
			}
			user.Committed = user.Committed || used
		}
		if user.Desired || user.Committed {
			users = append(users, user)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Session < users[j].Session })
	return users, issues
}

func usesConfigDirectory(root, path string) (bool, error) {
	canonical, err := config.CanonicalPath(path)
	if err != nil {
		return false, err
	}
	// Removing either a source's directory or an in-tree alias used to reach
	// it breaks the saved reference, even if the alias points outside the tree.
	for _, candidate := range []string{path, canonical} {
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			return false, err
		}
		if filepath.IsLocal(relative) {
			return true, nil
		}
	}
	return false, nil
}
