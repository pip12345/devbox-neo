package store

import (
	"context"
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
				record.Identity = journal.Source
				if entry.Name() == journal.Destination.Name {
					record.Identity = journal.Destination
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
				record := Record{Identity: j.Source}
				if name == j.Destination.Name {
					record.Identity = j.Destination
				}
				result = append(result, Entry{Name: name, Record: record, Pending: pending})
			}
		}
	}
	return result, nil
}

func (s *Store) LockAll(ctx context.Context, names []string) ([]*Locked, error) {
	// Caller supplies an already sorted, unique set. Lock order is global across
	// bulk operations and two-session transfers, not chosen per command.
	for i, name := range names {
		if i > 0 && names[i-1] >= name {
			return nil, os.ErrInvalid
		}
	}
	locks := make([]*Locked, 0, len(names))
	for _, name := range names {
		lock, err := s.Lock(ctx, name)
		if err != nil {
			for i := len(locks) - 1; i >= 0; i-- {
				locks[i].Close()
			}
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
