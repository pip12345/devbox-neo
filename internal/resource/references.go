package resource

import (
	"context"
	"errors"
	"fmt"
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

// ConfigUsers is advisory for editing and a current-state guard for deletion.
// It returns known users alongside errors: a corrupt record, pending transfer,
// or unresolved alias makes a complete usage report impossible.
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
		user := ConfigUse{Session: entry.Name}
		for _, reference := range entry.Record.Sources {
			source, err := reference.Expand(entry.Record.Identity.Workspace)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s source: %w", entry.Name, err))
				continue
			}
			path, err := config.CanonicalPath(source.Path)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s source: %w", entry.Name, err))
				continue
			}
			user.Desired = user.Desired || path == owner.Root
		}
		for _, source := range entry.Record.Inputs.Sources {
			path, err := config.CanonicalPath(source.Path)
			if err != nil {
				issues = errors.Join(issues, fmt.Errorf("session %s config source: %w", entry.Name, err))
				continue
			}
			user.Committed = user.Committed || path == owner.Root
		}
		if user.Desired || user.Committed {
			users = append(users, user)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Session < users[j].Session })
	return users, issues
}
