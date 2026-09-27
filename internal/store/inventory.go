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
		record, err := s.Read(ctx, entry.Name())
		pending, pendingErr := s.Pending(entry.Name())
		if pendingErr != nil {
			err = pendingErr
		}
		if pending != nil && os.IsNotExist(err) {
			journal, journalErr := s.ReadTransfer(pending.Source)
			if journalErr != nil {
				err = journalErr
			} else if journal != nil {
				record.Directory, record.ID = entry.Name(), journal.SourceID
				record.Settings.Binding = journal.Source.Binding
				if entry.Name() == journal.Destination.Name {
					record.Settings.Binding, record.ID = journal.Destination.Binding, journal.DestinationID
				}
				err = nil
			}
		}
		result = append(result, Entry{Name: entry.Name(), Record: record, Err: err, Pending: pending})
	}
	journals, err := s.Transfers()
	if err != nil {
		return nil, err
	}
	for _, j := range journals {
		for _, name := range []string{j.Source.Name, j.Destination.Name} {
			if seen[name] {
				continue
			}
			seen[name] = true
			pending, err := s.Pending(name)
			if err != nil {
				return nil, err
			}
			if pending != nil {
				record := Record{ID: j.SourceID, Directory: name, Settings: Settings{Binding: j.Source.Binding}}
				if name == j.Destination.Name {
					record.Settings.Binding, record.ID = j.Destination.Binding, j.DestinationID
				}
				result = append(result, Entry{Name: name, Record: record, Pending: pending})
			}
		}
	}
	return result, nil
}

// LockAll returns handles in directory order while acquiring unique session IDs
// in ID order. Move's two storage endpoints share one identity and one lock.
func (s *Store) LockAll(ctx context.Context, names []string, ids map[string]string) ([]*Locked, error) {
	byID := map[string]*Locked{}
	order := []string{}
	for i, name := range names {
		if (i > 0 && names[i-1] >= name) || !validName(name) || !idPattern.MatchString(ids[name]) {
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
