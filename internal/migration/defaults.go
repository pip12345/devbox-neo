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
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("preceding folder defaults path is not a directory")
		}
		return true, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	path, err = fsutil.Path(home, "state/folder-defaults.json")
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return false, nil // ordinary readers report corruption, not a fabricated migration
	}
	return header.Version == 1, nil
}

type defaultsConversion struct {
	lock         *store.LockedDefaults
	oldLocks     []*os.File
	state        store.FolderDefaults
	changed      bool
	paths        []string
	root         string
	hasDirectory bool
}

func (p *defaultsConversion) close() {
	if p.lock != nil {
		p.lock.Close()
	}
	for i := len(p.oldLocks) - 1; i >= 0; i-- {
		fsutil.Unlock(p.oldLocks[i])
	}
}

// prepareDefaults requires the namespace lock and follows session locks. It
// validates original ID associations before publishing shortened references.
func prepareDefaults(ctx context.Context, home string, origins map[string]string) (*defaultsConversion, error) {
	p := &defaultsConversion{state: store.FolderDefaults{Version: 2, Defaults: map[string]string{}}}
	ok := false
	defer func() {
		if !ok {
			p.close()
		}
	}()
	root, err := fsutil.Path(home, "state/workspaces")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	p.root, p.hasDirectory = root, err == nil
	p.changed = p.hasDirectory
	for _, entry := range entries {
		key := strings.TrimSuffix(entry.Name(), ".json")
		hash, err := hex.DecodeString(key)
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != key {
			return nil, fmt.Errorf("unexpected preceding default entry: %s", entry.Name())
		}
		dir, err := fsutil.Dir(home, "state/locks/workspaces", 0700)
		if err != nil {
			return nil, err
		}
		lock, err := fsutil.Lock(ctx, filepath.Join(dir, key+".lock"))
		if err != nil {
			return nil, err
		}
		p.oldLocks = append(p.oldLocks, lock)
	}
	s := &store.Store{Home: home}
	p.lock, err = s.LockDefaults(ctx)
	if err != nil {
		return nil, err
	}
	shorten := func(id string) (string, error) {
		if !legacyID(id) {
			return "", fmt.Errorf("invalid preceding default session ID")
		}
		key := id[:12]
		if original, exists := origins[key]; exists && original != id {
			return "", fmt.Errorf("default identity conflicts with conversion destination %s", key)
		}
		return key, nil
	}
	global, err := fsutil.Path(home, "state/folder-defaults.json")
	if err != nil {
		return nil, err
	}
	data, readErr := os.ReadFile(global)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if readErr == nil {
		var state store.FolderDefaults
		if err := config.Decode(data, &state); err != nil {
			return nil, err
		}
		if state.Version == 2 {
			if err := state.Validate(); err != nil {
				return nil, err
			}
			p.state = state
		} else if state.Version == 1 && state.Defaults != nil {
			for workspace, id := range state.Defaults {
				if !validSavedFolder(workspace) {
					return nil, fmt.Errorf("invalid preceding default workspace")
				}
				key, err := shorten(id)
				if err != nil {
					return nil, err
				}
				p.state.Defaults[workspace] = key
			}
			p.changed = true
		} else {
			return nil, fmt.Errorf("unsupported folder defaults format")
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := fsutil.Path(root, entry.Name())
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var old struct {
			Version   int             `json:"version"`
			Workspace string          `json:"workspace"`
			Default   json.RawMessage `json:"default_session"`
		}
		if err := config.Decode(data, &old); err != nil {
			return nil, err
		}
		hash := sha256.Sum256([]byte(old.Workspace))
		if old.Version != 2 || !validSavedFolder(old.Workspace) || old.Default == nil || entry.Name() != hex.EncodeToString(hash[:])+".json" {
			return nil, fmt.Errorf("invalid preceding folder default identity")
		}
		if !bytes.Equal(bytes.TrimSpace(old.Default), []byte("null")) {
			var selected store.DefaultSession
			if err := config.Decode(old.Default, &selected); err != nil {
				return nil, err
			}
			key, err := shorten(selected.ID)
			if err != nil {
				return nil, err
			}
			if current := p.state.Defaults[old.Workspace]; current != "" && current != key {
				return nil, fmt.Errorf("folder default conflicts with converted selection: %s", old.Workspace)
			}
			p.state.Defaults[old.Workspace] = key
		}
		p.paths = append(p.paths, path)
		p.changed = true
	}
	if err := p.state.Validate(); err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

func (p *defaultsConversion) publish(ctx context.Context) error {
	if !p.changed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.lock.Save(p.state); err != nil {
		return err
	}
	for _, path := range p.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if p.hasDirectory {
		if err := os.Remove(p.root); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(p.root))
	}
	return nil
}

func validSavedFolder(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, '\x00')
}

func migrateFolderDefaults(ctx context.Context, home string) error {
	s := &store.Store{Home: home}
	if _, err := fsutil.Dir(home, "state/locks", 0700); err != nil {
		return err
	}
	names, err := s.LockNames(ctx)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(names)
	if err := requireResolvedTransfers(home); err != nil {
		return err
	}
	origins := map[string]string{}
	root, err := fsutil.Path(home, "sessions")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		path, err := fsutil.Path(root, filepath.Join(entry.Name(), "session.json"))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var header struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return err
		}
		if header.Version == 6 || header.Version == 7 {
			if !legacyID(header.ID) {
				return fmt.Errorf("invalid preceding identity")
			}
			origins[header.ID[:12]] = header.ID
		} else if header.Version == store.RecordVersion && environment.IsSessionID(header.ID) {
			origins[header.ID] = header.ID
		}
	}
	plan, err := prepareDefaults(ctx, home, origins)
	if err != nil {
		return err
	}
	defer plan.close()
	return plan.publish(ctx)
}
