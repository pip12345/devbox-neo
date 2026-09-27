package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
)

// LockNames serializes changes to the folder-local name namespace. Acquire it
// before session operation locks; ordinary access never needs this lock.
func (s *Store) LockNames(ctx context.Context) (*os.File, error) {
	return fsutil.Lock(ctx, filepath.Join(s.Home, "state/locks/names.lock"))
}

func (s *Store) Find(ctx context.Context, id string, binding *environment.Binding) (Record, error) {
	if (id == "") == (binding == nil) {
		return Record{}, fmt.Errorf("select a session ID or workspace/name")
	}
	if id != "" && !environment.IsSessionTarget(id) {
		return Record{}, fmt.Errorf("invalid session ID")
	}
	if binding != nil {
		if err := binding.Validate(); err != nil {
			return Record{}, err
		}
	}
	{
		journals, err := s.Transfers()
		if err != nil {
			return Record{}, err
		}
		var pending *Transfer
		for i := range journals {
			j := &journals[i]
			matches := id != "" && (j.SourceID == id || j.DestinationID == id)
			if binding != nil { matches = j.Source.Binding == *binding || j.Destination.Binding == *binding }
			if !matches {
				continue
			}
			if pending != nil {
				return Record{}, fmt.Errorf("conflicting transfer journals")
			}
			pending = j
		}
		if pending != nil {
			j := pending
			directory := j.Source.Name
			if (binding != nil && j.Destination.Binding == *binding) || (id != "" && j.DestinationID == id && (j.SourceID != id || j.Phase == "committed")) {
				directory = j.Destination.Name
			}
			r, err := s.Read(ctx, directory)
			if os.IsNotExist(err) {
				return r, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", id, err, j.RetryStep())
			}
			if err == nil && ((id != "" && r.ID != id) || (binding != nil && r.Settings.Binding != *binding)) {
				return r, fmt.Errorf("reserved session identity changed")
			}
			return r, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.Home, "sessions"))
	if err != nil {
		return Record{}, err
	}
	var found Record
	var unreadable error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		r, err := s.Read(ctx, entry.Name())
		matches := (id == "" || r.ID == id) && (binding == nil || r.Settings.Binding == *binding)
		if err != nil {
			if matches && r.ID != "" {
				return r, err
			}
			unreadable = fmt.Errorf("cannot complete session lookup: %w", err)
			continue // A readable match is not blocked by unrelated broken records.
		}
		if !matches {
			continue
		}
		if found.ID != "" {
			return Record{}, fmt.Errorf("ambiguous session selection; inspect session inventory")
		}
		found = r
	}
	if found.ID == "" {
		if unreadable != nil {
			return Record{}, commanderror.New("inventory_incomplete", "Session inventory contains unreadable records; inspect list.", "", unreadable, commanderror.Next("Inspect saved state", "list"))
		}
		return Record{}, os.ErrNotExist
	}
	return found, nil
}

// RequireUnusedBinding runs under LockNames. Pending endpoints reserve their
// selected name even before their session record has been committed.
func (s *Store) RequireUnusedBinding(ctx context.Context, binding environment.Binding, exceptDirectory string) error {
	journals, err := s.Transfers()
	if err != nil {
		return err
	}
	for _, j := range journals {
		for _, endpoint := range []environment.Identity{j.Source, j.Destination} {
			if endpoint.Name != exceptDirectory && endpoint.Binding == binding {
				return commanderror.New("pending_transfer", "Session name is reserved by a pending transfer.", j.SourceID, nil, j.RetryStep())
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.Home, "sessions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == exceptDirectory {
			continue
		}
		r, err := s.Read(ctx, entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot check session names: %w", err)
		}
		if r.Settings.Binding == binding {
			return commanderror.New("session_exists", "Session already exists for this workspace and name.", r.ID, nil, commanderror.Next("Inspect the existing session", "status", r.ID))
		}
	}
	return nil
}

func AllocateDirectory(binding environment.Binding) (string, error) {
	nonce, err := fsutil.ID()
	if err != nil {
		return "", err
	}
	return environment.ResourceName(binding.Workspace, binding.LocalName, nonce), nil
}
