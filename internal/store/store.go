// Package store owns durable sessions and their external mutation locks.
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
)

type Store struct {
	Home         string
	Installation string
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}
func Open(ctx context.Context, home string) (*Store, error) {
	absolute, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if _, err = fsutil.Dir(absolute, "state/locks/sessions", 0700); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(absolute, "state/locks/installation.lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	p, err := fsutil.Path(absolute, "state/installation-id")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		id, e := fsutil.ID()
		if e != nil {
			return nil, e
		}
		if err = fsutil.Write(p, []byte(id+"\n"), 0600); err != nil {
			return nil, err
		}
		b = []byte(id)
	} else if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(b))
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("corrupt installation identity")
	}
	for _, dir := range []string{"configs", "harnesses", "auth", "cache/harnesses", "sessions"} {
		if _, err = fsutil.Dir(absolute, dir, 0700); err != nil {
			return nil, err
		}
	}
	return &Store{Home: absolute, Installation: id}, nil
}

type operationLock struct{ file *os.File }
type Locked struct {
	ctx       context.Context
	store     *Store
	Name      string
	ID        string
	operation *operationLock
}

func (s *Store) Lock(ctx context.Context, directory, id string) (*Locked, error) {
	if !validName(directory) || !idPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid session lock reference")
	}
	operation, err := s.lockID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Locked{ctx: ctx, store: s, Name: directory, ID: id, operation: operation}, nil
}

func (s *Store) lockID(ctx context.Context, id string) (*operationLock, error) {
	path, err := fsutil.Path(s.Home, filepath.Join("state/locks/sessions", id+".lock"))
	if err != nil {
		return nil, err
	}
	f, err := fsutil.Lock(ctx, path)
	if err != nil {
		return nil, err
	}
	return &operationLock{file: f}, nil
}
func (l *Locked) Close() error {
	if !l.Held() {
		return nil
	}
	err := fsutil.Unlock(l.operation.file)
	l.operation.file = nil
	return err
}
func (l *Locked) Held() bool { return l.operation != nil && l.operation.file != nil }
func (l *Locked) check() error {
	if !l.Held() {
		return fmt.Errorf("session operation lock is not held")
	}
	return nil
}
func (l *Locked) Path(rel string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("session path must remain relative")
	}
	return fsutil.Path(l.store.Home, filepath.Join("sessions", l.Name, rel))
}
func (l *Locked) Dir(rel string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("session path must remain relative")
	}
	return fsutil.Dir(l.store.Home, filepath.Join("sessions", l.Name, rel), 0700)
}
func (l *Locked) Load() (Record, error) {
	if err := l.RequireAvailable(); err != nil {
		return Record{}, err
	}
	return l.ReadRecord(l.ctx)
}
func (s *Store) Read(ctx context.Context, name string) (Record, error) {
	var record Record
	if err := ctx.Err(); err != nil {
		return record, err
	}
	if !validName(name) {
		return record, fmt.Errorf("invalid session directory")
	}
	path, err := fsutil.Path(s.Home, filepath.Join("sessions", name, "session.json"))
	if err != nil {
		return record, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err = config.Decode(b, &record); err != nil {
		return record, commanderror.New("invalid_session_record", fmt.Sprintf("Invalid session state: %v", err), path, err)
	}
	record.Directory = name
	if err = record.Validate(name); err != nil {
		return record, commanderror.New("invalid_session_record", "Invalid session state: "+err.Error(), path, err)
	}
	return record, nil
}
func (l *Locked) Save(record Record) error {
	if err := l.check(); err != nil {
		return err
	}
	if record.Directory != l.Name || record.ID != l.ID {
		return fmt.Errorf("record belongs to a different storage directory")
	}
	if err := record.Validate(l.Name); err != nil {
		return err
	}
	if _, err := l.Dir("."); err != nil {
		return err
	}
	path, err := l.Path("session.json")
	if err != nil {
		return err
	}
	return fsutil.JSON(path, record)
}

// The ID-keyed operation lock survives deletion. Readers see an atomic record
// or absence; mutating callers always reload after acquiring the operation lock.
func (l *Locked) Delete() error { return l.DeleteContext(l.ctx) }

// DeleteContext lets bounded cleanup retain the operation lock after the
// foreground context has been cancelled.
func (l *Locked) DeleteContext(ctx context.Context) error {
	if err := l.check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := l.Path(".")
	if err != nil {
		return err
	}
	if err = os.RemoveAll(root); err != nil {
		return err
	}
	leases, err := l.leaseDirectory(false)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(leases); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Touch reloads under the operation lock so a long attached command cannot
// overwrite creation/runtime settings committed by a more recent invocation.
func (l *Locked) Touch(id, action string) (Record, error) {
	r, err := l.Load()
	if err != nil {
		return Record{}, err
	}
	if r.ID != id {
		return Record{}, fmt.Errorf("session identity changed while attached")
	}
	r.Activity = time.Now().UTC()
	r.Action = action
	return r, l.Save(r)
}
