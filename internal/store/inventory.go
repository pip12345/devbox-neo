package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

// LockAll acquires the complete unique ID set in sorted order. Move uses one
// handle for its source and destination trees.
func (s *Store) LockAll(ctx context.Context, names []string, ids map[string]string) ([]*Locked, error) {
	byID := map[string]*Locked{}
	order := []string{}
	for i, name := range names {
		if (i > 0 && names[i-1] >= name) || name != ids[name] || !idPattern.MatchString(ids[name]) {
			return nil, fmt.Errorf("invalid session lock set")
		}
		if _, seen := byID[ids[name]]; !seen {
			byID[ids[name]] = nil
			order = append(order, ids[name])
		}
	}
	sort.Strings(order)
	acquired := []*Locked{}
	for _, id := range order {
		operation, err := s.lockID(ctx, id)
		if err != nil {
			CloseAll(acquired)
			return nil, err
		}
		lock := &Locked{ctx: ctx, store: s, ID: id, operation: operation}
		byID[id] = lock
		acquired = append(acquired, lock)
	}
	result := make([]*Locked, 0, len(names))
	for _, name := range names {
		result = append(result, &Locked{ctx: ctx, store: s, Name: name, ID: ids[name], operation: byID[ids[name]].operation})
	}
	return result, nil
}
func CloseAll(locks []*Locked) {
	for i := len(locks) - 1; i >= 0; i-- {
		locks[i].Close()
	}
}
func (s *Store) RecordPath(name string) (string, error) {
	return fsutil.Path(s.Home, filepath.Join("sessions", name, "session.json"))
}
