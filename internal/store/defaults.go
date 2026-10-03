package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
)

// DefaultSession selects durable identity, independently of names and storage.
type DefaultSession struct {
	ID string `json:"id"`
}

// FolderDefaults stores only selected folders; absence means no default.
type FolderDefaults struct {
	Version  int               `json:"version"`
	Defaults map[string]string `json:"defaults"`
}

func validDefaultFolder(workspace string) bool {
	// Saved folder identity must remain usable even after the folder is removed.
	return filepath.IsAbs(workspace) && filepath.Clean(workspace) == workspace && !strings.ContainsRune(workspace, '\x00')
}

func (d FolderDefaults) Validate() error {
	if d.Version != 1 || d.Defaults == nil {
		return fmt.Errorf("invalid folder defaults format")
	}
	for workspace, id := range d.Defaults {
		if !validDefaultFolder(workspace) || !idPattern.MatchString(id) {
			return fmt.Errorf("invalid folder default selection")
		}
	}
	return nil
}

// LockedDefaults owns the entire mapping so updates for different folders
// cannot overwrite each other. Acquire it after any session locks; never
// acquire a session lock while holding it.
type LockedDefaults struct {
	store *Store
	file  *os.File
}

func (s *Store) LockDefaults(ctx context.Context) (*LockedDefaults, error) {
	dir, err := fsutil.Dir(s.Home, "state/locks", 0700)
	if err != nil {
		return nil, err
	}
	file, err := fsutil.Lock(ctx, filepath.Join(dir, "folder-defaults.lock"))
	if err != nil {
		return nil, err
	}
	return &LockedDefaults{store: s, file: file}, nil
}

func (l *LockedDefaults) Close() error {
	if l.file == nil {
		return nil
	}
	err := fsutil.Unlock(l.file)
	l.file = nil
	return err
}

func (l *LockedDefaults) path() (string, error) {
	if l.file == nil {
		return "", fmt.Errorf("folder defaults lock is not held")
	}
	return fsutil.Path(l.store.Home, "state/folder-defaults.json")
}

func (l *LockedDefaults) Read() (FolderDefaults, error) {
	state := FolderDefaults{Version: 1, Defaults: map[string]string{}}
	path, err := l.path()
	if err != nil {
		return state, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	// Decode into a zero value so a missing defaults field cannot look empty.
	state = FolderDefaults{}
	if err = config.Decode(data, &state); err == nil {
		err = state.Validate()
	}
	if err != nil {
		return state, commanderror.New("invalid_folder_defaults", "Invalid folder defaults: "+err.Error(), path, err)
	}
	return state, nil
}

// Save publishes current-format data only. The migration service uses the same
// validation and atomic publication as ordinary default selection.
func (l *LockedDefaults) Save(state FolderDefaults) error {
	path, err := l.path()
	if err != nil {
		return err
	}
	if err := state.Validate(); err != nil {
		return err
	}
	return fsutil.JSON(path, state)
}

func (l *LockedDefaults) set(workspace, id string) error {
	if !validDefaultFolder(workspace) || (id != "" && !idPattern.MatchString(id)) {
		return fmt.Errorf("invalid folder default selection")
	}
	state, err := l.Read()
	if err != nil {
		return err
	}
	if state.Defaults[workspace] == id {
		return nil
	}
	if id == "" {
		delete(state.Defaults, workspace)
	} else {
		state.Defaults[workspace] = id
	}
	return l.Save(state)
}

// ReadDefault releases the mapping lock before callers acquire a session lock.
// The selected ID is an invocation snapshot, not a target that can change later.
func (s *Store) ReadDefault(ctx context.Context, workspace string) (*DefaultSession, error) {
	if !validDefaultFolder(workspace) {
		return nil, fmt.Errorf("invalid folder identity")
	}
	lock, err := s.LockDefaults(ctx)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	state, err := lock.Read()
	if err != nil {
		return nil, err
	}
	if id := state.Defaults[workspace]; id != "" {
		return &DefaultSession{ID: id}, nil
	}
	return nil, nil
}

// Clearing an absent selection does not seed a file or select a replacement.
func (s *Store) ClearDefault(ctx context.Context, workspace string) error {
	lock, err := s.LockDefaults(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	return lock.set(workspace, "")
}

func (l *Locked) SelectDefault(expectedID string) error {
	record, err := l.Load()
	if err != nil {
		return err
	}
	if record.ID != expectedID {
		return commanderror.New("session_changed", "Session identity changed; select it again.", l.Name, nil)
	}
	lock, err := l.store.LockDefaults(l.ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	return lock.set(record.Settings.Workspace, record.ID)
}

// ClearMatchingDefault also works after committed move cleanup removed the
// source record. Never clear a newer selection made for the same folder.
func (l *Locked) ClearMatchingDefault(ctx context.Context, workspace, id string) error {
	if err := l.check(); err != nil {
		return err
	}
	if id != l.ID || !idPattern.MatchString(id) || !validDefaultFolder(workspace) {
		return fmt.Errorf("default cleanup identity differs from the operation lock")
	}
	lock, err := l.store.LockDefaults(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := lock.Read()
	if err != nil {
		return err
	}
	if state.Defaults[workspace] != id {
		return nil
	}
	delete(state.Defaults, workspace)
	return lock.Save(state)
}
