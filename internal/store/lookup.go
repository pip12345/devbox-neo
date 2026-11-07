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

// LockNames serializes workspace/name reservations and new directory claims.
// Acquire it before session operation locks.
func (s *Store) LockNames(ctx context.Context) (*os.File, error) {
	return fsutil.Lock(ctx, filepath.Join(s.Home, "state/locks/names.lock"))
}

func (s *Store) Find(ctx context.Context, id string, binding *environment.Binding) (Record, error) {
	if (id == "") == (binding == nil) {
		return Record{}, fmt.Errorf("select a session ID or workspace/name")
	}
	if id != "" && !environment.IsSessionID(id) {
		return Record{}, fmt.Errorf("invalid session ID")
	}
	if binding != nil {
		if err := binding.Validate(); err != nil {
			return Record{}, err
		}
	}
	journals, err := s.Transfers()
	if err != nil {
		return Record{}, err
	}
	var pending *Transfer
	for i := range journals {
		j := &journals[i]
		if (id != "" && (j.SourceID == id || j.DestinationID == id)) ||
			(binding != nil && (j.Source == *binding || j.Destination == *binding)) {
			if pending != nil {
				return Record{}, fmt.Errorf("conflicting transfer journals")
			}
			pending = j
		}
	}
	if pending != nil {
		j := *pending
		selected := j.SourceID
		if (id == j.DestinationID && id != j.SourceID) ||
			(binding != nil && *binding == j.Destination) ||
			(j.Mode == "relocate" && j.Phase == "committed") {
			selected = j.DestinationID
		}
		var r Record
		if selected == j.DestinationID && (j.SourceID != j.DestinationID || j.Phase == "committed" || (binding != nil && *binding == j.Destination)) {
			r, err = s.ReadTransferDestination(ctx, j)
		} else {
			r, err = s.Read(ctx, selected)
		}
		if os.IsNotExist(err) {
			return r, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", j.SourceName(), err, j.RetryStep())
		}
		if err == nil && ((id != "" && r.ID != id) || (binding != nil && r.Settings.Binding != *binding)) {
			return r, commanderror.New("pending_transfer", "The selected endpoint is reserved by an unfinished transfer.", j.SourceName(), nil, j.RetryStep())
		}
		return r, err
	}
	if id != "" {
		return s.Read(ctx, id)
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
		if err != nil {
			if r.ID != "" && r.Settings.Binding == *binding {
				return r, err
			}
			unreadable = err
			continue
		}
		if r.Settings.Binding != *binding {
			continue
		}
		if found.ID != "" {
			return Record{}, fmt.Errorf("ambiguous session selection; inspect session inventory")
		}
		found = r
	}
	if found.ID != "" {
		return found, nil
	}
	if unreadable != nil {
		return Record{}, commanderror.New("inventory_incomplete", "Session inventory contains unreadable records; inspect list.", "", unreadable, commanderror.Next("Inspect saved state", "list"))
	}
	return Record{}, os.ErrNotExist
}

// RequireUnusedBinding runs under LockNames. Journal endpoints reserve names
// even while their destination record is still private preparation state.
func (s *Store) RequireUnusedBinding(ctx context.Context, binding environment.Binding, exceptID string) error {
	journals, err := s.Transfers()
	if err != nil {
		return err
	}
	for _, j := range journals {
		if j.SourceID != exceptID && j.Source == binding || j.DestinationID != exceptID && j.Destination == binding {
			return commanderror.New("pending_transfer", "Session name is reserved by a pending transfer.", j.SourceName(), nil, j.RetryStep())
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.Home, "sessions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == exceptID {
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
			return commanderror.New("session_exists", "Session already exists for this workspace and name.", r.PublicName(), nil, commanderror.Next("Inspect the existing session", "status", r.PublicName()))
		}
	}
	return nil
}
