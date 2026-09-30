package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
)

// IncompleteDirectory is a snapshot of a directory with no session record.
// It has no session identity and cannot authorize container, image, default,
// or lease mutations. Check and Remove require the name-namespace lock, which
// creation and transfer publication also hold before allocating directories.
type IncompleteDirectory struct {
	Name  string
	Path  string
	store *Store
	info  os.FileInfo
}

func (s *Store) InspectIncompleteDirectory(ctx context.Context, name string) (*IncompleteDirectory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validName(name) {
		return nil, fmt.Errorf("invalid session directory")
	}
	root, err := fsutil.Path(s.Home, filepath.Join("sessions", name))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("session storage is not a directory: %s", root)
	}
	// Any record entry, even corrupt or a dangling symlink, prevents this
	// cleanup path from treating applied session state as an abandoned copy.
	if _, err := os.Lstat(filepath.Join(root, "session.json")); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	pending, err := s.Pending(name)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		return nil, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", name, nil, pending.RetryStep())
	}
	return &IncompleteDirectory{Name: name, Path: root, store: s, info: info}, nil
}

func (d *IncompleteDirectory) Check(ctx context.Context) error {
	current, err := d.store.InspectIncompleteDirectory(ctx, d.Name)
	if err != nil {
		return err
	}
	if current == nil || !os.SameFile(d.info, current.info) {
		return fmt.Errorf("incomplete directory changed since selection; preview deletion again: %s", d.Name)
	}
	return nil
}

func (d *IncompleteDirectory) Remove(ctx context.Context) error {
	if err := d.Check(ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(d.Path); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(d.Path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
