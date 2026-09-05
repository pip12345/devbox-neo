package store

import (
	"context"
	"os"
	"path/filepath"

	"devbox/internal/fsutil"
)

type Entry struct {
	Name   string
	Record Record
	Err    error
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
	for _, entry := range entries {
		record, err := s.Read(ctx, entry.Name())
		result = append(result, Entry{Name: entry.Name(), Record: record, Err: err})
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
