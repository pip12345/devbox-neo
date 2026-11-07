package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/fsutil"
)

type Entry struct {
	Name    string
	Record  Record
	Err     error
	Pending *Reservation
}

// Inventory exposes corrupt entries instead of treating them as missing. A
// caller selecting one known identity need not load unrelated session records.
func (s *Store) Inventory(ctx context.Context) ([]Entry, error) {
	root, err := fsutil.Path(s.Home, "sessions")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Name()] = true
		record, readErr := s.Read(ctx, entry.Name())
		result = append(result, Entry{Name: entry.Name(), Record: record, Err: readErr})
	}
	journals, err := s.Transfers()
	if err != nil {
		return nil, err
	}
	for _, j := range journals {
		ids := []string{j.SourceID}
		if j.DestinationID != j.SourceID {
			ids = append(ids, j.DestinationID)
		}
		for _, id := range ids {
			var r Record
			var readErr error
			if id == j.DestinationID && (id != j.SourceID || j.Phase == "committed") {
				r, readErr = s.ReadTransferDestination(ctx, j)
				if os.IsNotExist(readErr) {
					r = Record{ID: id, Directory: id, StateTree: j.DestinationTree, Settings: Settings{Binding: j.Destination}}
					readErr = nil
				}
			} else {
				r, readErr = s.Read(ctx, id)
			}
			entry := Entry{Name: id, Record: r, Err: readErr, Pending: j.Reservation()}
			if seen[id] {
				for i := range result {
					if result[i].Name == id {
						result[i] = entry
					}
				}
			} else {
				seen[id] = true
				result = append(result, entry)
			}
		}
	}
	return result, nil
}

// LockAll requires sorted, unique session IDs so overlapping operations acquire
// locks in the same order. Failure releases every lock acquired by this call.
func (s *Store) LockAll(ctx context.Context, ids []string) ([]*Locked, error) {
	for i, id := range ids {
		if !idPattern.MatchString(id) || (i > 0 && ids[i-1] >= id) {
			return nil, fmt.Errorf("invalid session lock set")
		}
	}
	locks := make([]*Locked, 0, len(ids))
	for _, id := range ids {
		lock, err := s.Lock(ctx, id)
		if err != nil {
			CloseAll(locks)
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, nil
}
func CloseAll(locks []*Locked) {
	for i := len(locks) - 1; i >= 0; i-- {
		locks[i].Close()
	}
}
func (s *Store) RecordPath(name string) (string, error) {
	return fsutil.Path(s.Home, filepath.Join("sessions", name, "session.json"))
}
