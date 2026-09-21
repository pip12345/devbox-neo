package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
)

// DefaultSession pins durable identity as well as the lookup name. Reusing a
// deleted session's local name must not silently inherit its default selection.
type DefaultSession struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type workspaceState struct {
	Version        int             `json:"version"`
	Workspace      string          `json:"workspace"`
	DefaultSession *DefaultSession `json:"default_session"`
}

type workspaceLock struct {
	store     *Store
	workspace string
	key       string
	file      *os.File
}

// WorkspaceKey takes an already canonical identity, without requiring its
// directory to remain accessible. Deletion and transfer cleanup use saved paths.
func WorkspaceKey(workspace string) (string, error) {
	if !filepath.IsAbs(workspace) || filepath.Clean(workspace) != workspace || strings.ContainsRune(workspace, '\x00') {
		return "", fmt.Errorf("workspace identity must be a clean absolute path")
	}
	sum := sha256.Sum256([]byte(workspace))
	return hex.EncodeToString(sum[:]), nil
}

// Workspace locks are always acquired after session operation locks, if any.
// The owner must never acquire a session lock before releasing this lock.
func (s *Store) lockWorkspace(ctx context.Context, workspace string) (*workspaceLock, error) {
	key, err := WorkspaceKey(workspace)
	if err != nil {
		return nil, err
	}
	dir, err := fsutil.Dir(s.Home, "state/locks/workspaces", 0700)
	if err != nil {
		return nil, err
	}
	file, err := fsutil.Lock(ctx, filepath.Join(dir, key+".lock"))
	if err != nil {
		return nil, err
	}
	return &workspaceLock{store: s, workspace: workspace, key: key, file: file}, nil
}

func (l *workspaceLock) close() error {
	if l.file == nil {
		return nil
	}
	err := fsutil.Unlock(l.file)
	l.file = nil
	return err
}

func (l *workspaceLock) path() (string, error) {
	if l.file == nil {
		return "", fmt.Errorf("workspace lock is not held")
	}
	return fsutil.Path(l.store.Home, filepath.Join("state/workspaces", l.key+".json"))
}

func (l *workspaceLock) load() (*workspaceState, error) {
	path, err := l.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state workspaceState
	if err = config.Decode(data, &state); err == nil {
		var fields map[string]json.RawMessage
		err = json.Unmarshal(data, &fields)
		if err == nil && (state.Version != 1 || state.Workspace != l.workspace || fields["default_session"] == nil) {
			err = fmt.Errorf("workspace state does not match its version or workspace identity")
		}
		if err == nil && state.DefaultSession != nil {
			err = state.DefaultSession.validate()
		}
	}
	if err != nil {
		return nil, commanderror.New("invalid_workspace_state", "Invalid folder default state: "+err.Error(), path, err)
	}
	return &state, nil
}

func (d DefaultSession) validate() error {
	if !validName(d.Name) || !idPattern.MatchString(d.ID) {
		return fmt.Errorf("invalid default session identity")
	}
	return nil
}

func (l *workspaceLock) set(selected *DefaultSession) error {
	if selected != nil {
		if err := selected.validate(); err != nil {
			return err
		}
	}
	state, err := l.load()
	if err != nil {
		return err
	}
	if state == nil {
		if selected == nil {
			return nil
		}
		state = &workspaceState{Version: 1, Workspace: l.workspace}
	}
	state.DefaultSession = selected
	if _, err := fsutil.Dir(l.store.Home, "state/workspaces", 0700); err != nil {
		return err
	}
	path, err := l.path()
	if err != nil {
		return err
	}
	return fsutil.JSON(path, state)
}

// ReadDefault returns an invocation snapshot. It releases the workspace lock
// before the caller acquires a session operation lock and checks the saved ID.
func (s *Store) ReadDefault(ctx context.Context, workspace string) (*DefaultSession, error) {
	lock, err := s.lockWorkspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer lock.close()
	state, err := lock.load()
	if err != nil || state == nil {
		return nil, err
	}
	return state.DefaultSession, nil
}

// ClearDefault needs no session or configuration access. An absent state file
// remains absent; clearing never creates a default or chooses a replacement.
func (s *Store) ClearDefault(ctx context.Context, workspace string) error {
	lock, err := s.lockWorkspace(ctx, workspace)
	if err != nil {
		return err
	}
	defer lock.close()
	return lock.set(nil)
}

func (l *Locked) SelectDefault(expectedID string) error {
	record, err := l.Load()
	if err != nil {
		return err
	}
	if record.ID != expectedID {
		return commanderror.New("session_changed", "Session identity changed; select it again.", l.Name, nil)
	}
	lock, err := l.store.lockWorkspace(l.ctx, record.Identity.Workspace)
	if err != nil {
		return err
	}
	defer lock.close()
	return lock.set(&DefaultSession{Name: l.Name, ID: record.ID})
}

// ClearMatchingDefault is part of saved-state removal, not container removal.
// It also works after a committed move deleted the source record: the journal
// supplies workspace and ID. The session lock excludes a concurrent reselection.
func (l *Locked) ClearMatchingDefault(ctx context.Context, workspace, id string) error {
	if err := l.check(); err != nil {
		return err
	}
	selected := DefaultSession{Name: l.Name, ID: id}
	if err := selected.validate(); err != nil {
		return err
	}
	lock, err := l.store.lockWorkspace(ctx, workspace)
	if err != nil {
		return err
	}
	defer lock.close()
	state, err := lock.load()
	if err != nil || state == nil || state.DefaultSession == nil {
		return err
	}
	if *state.DefaultSession != selected {
		return nil
	}
	return lock.set(nil)
}
