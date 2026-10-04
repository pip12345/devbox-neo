package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func defaultsPending(home string) (bool, error) {
	path, err := fsutil.Path(home, "state/workspaces")
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("old folder defaults path is not a directory")
	}
	return true, nil
}

// Conversion publishes the complete mapping before removing any old files.
// Remaining files after interruption can only contribute identical selections
// on retry; a conflict is an error, not permission to replace a newer choice.
func migrateFolderDefaults(ctx context.Context, home string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pending, err := defaultsPending(home); err != nil || !pending {
		return err
	}
	if _, err := fsutil.Dir(home, "state/locks", 0700); err != nil {
		return err
	}
	s := &store.Store{Home: home}
	names, err := s.LockNames(ctx)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(names)
	root, err := fsutil.Path(home, "state/workspaces")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var oldLocks []*os.File
	defer func() {
		for i := len(oldLocks) - 1; i >= 0; i-- {
			fsutil.Unlock(oldLocks[i])
		}
	}()
	for _, entry := range entries {
		key := strings.TrimSuffix(entry.Name(), ".json")
		hash, err := hex.DecodeString(key)
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != key {
			return fmt.Errorf("unexpected old folder defaults entry: %s", entry.Name())
		}
		dir, err := fsutil.Dir(home, "state/locks/workspaces", 0700)
		if err != nil {
			return err
		}
		lock, err := fsutil.Lock(ctx, filepath.Join(dir, key+".lock"))
		if err != nil {
			return err
		}
		oldLocks = append(oldLocks, lock)
	}
	lock, err := s.LockDefaults(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := lock.Read()
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := fsutil.Path(root, entry.Name())
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var old struct {
			Version   int             `json:"version"`
			Workspace string          `json:"workspace"`
			Default   json.RawMessage `json:"default_session"`
		}
		if err := config.Decode(data, &old); err != nil {
			return fmt.Errorf("invalid old folder default %s: %w", path, err)
		}
		hash := sha256.Sum256([]byte(old.Workspace))
		if old.Version != 2 || old.Default == nil || !filepath.IsAbs(old.Workspace) || filepath.Clean(old.Workspace) != old.Workspace || strings.ContainsRune(old.Workspace, '\x00') || entry.Name() != hex.EncodeToString(hash[:])+".json" {
			return fmt.Errorf("invalid old folder default identity: %s", path)
		}
		if !bytes.Equal(bytes.TrimSpace(old.Default), []byte("null")) {
			var selected store.DefaultSession
			if err := config.Decode(old.Default, &selected); err != nil {
				return fmt.Errorf("invalid old default selection %s: %w", path, err)
			}
			if !environment.IsSessionID(selected.ID) {
				return fmt.Errorf("invalid old default session ID: %s", path)
			}
			if current := state.Defaults[old.Workspace]; current != "" && current != selected.ID {
				return fmt.Errorf("folder default conflicts with the converted selection: %s", old.Workspace)
			}
			state.Defaults[old.Workspace] = selected.ID
		}
		paths = append(paths, path)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := lock.Save(state); err != nil {
		return err
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	// Remove only the emptied source directory, never unknown/new entries.
	if err := os.Remove(root); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
