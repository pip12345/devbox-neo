// Package filesync projects desired configuration without taking ownership of user edits.
// The caller holds the operation lock and proves the container stopped or absent.
package filesync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

type Entry struct {
	Source    string            `json:"source"`
	Layer     string            `json:"layer"`
	Strategy  string            `json:"strategy"`
	Store     string            `json:"store"`
	Path      string            `json:"path"`
	Hash      string            `json:"hash,omitempty"`
	Mode      os.FileMode       `json:"mode,omitempty"`
	AppliedAt time.Time         `json:"applied_at,omitempty"`
	Keys      []string          `json:"owned_keys,omitempty"`
	KeyHashes map[string]string `json:"key_hashes,omitempty"`
	Conflict  bool              `json:"conflict,omitempty"`
}
type Manifest struct {
	Version int              `json:"version"`
	Files   map[string]Entry `json:"files"`
}
type Conflicts struct{ Paths []string }

func (c *Conflicts) Error() string {
	return fmt.Sprintf("Configuration conflicts at %v; live files preserved.", c.Paths)
}
func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func object(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := config.Decode(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return m, nil
}

// Validate checks desired content without reading or changing destination state.
func Validate(desired map[string]artifact.File, merges []harness.Merge) error {
	rules := map[string]bool{}
	for _, r := range merges {
		rules[r.Path] = true
	}
	for p, f := range desired {
		if p == "." || !filepath.IsLocal(p) {
			return fmt.Errorf("managed config path must remain relative")
		}
		if rules[p] {
			if _, err := object(f.Data); err != nil {
				return fmt.Errorf("desired %s must be a valid JSON object", p)
			}
		}
	}
	return nil
}

func loadManifest(path string) (Manifest, error) {
	m := Manifest{Version: 1, Files: map[string]Entry{}}
	if _, err := fsutil.Path(filepath.Dir(path), filepath.Base(path)); err != nil {
		return m, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err = config.Decode(b, &m); err != nil {
		return m, fmt.Errorf("invalid managed manifest: %w", err)
	}
	if m.Version != 1 || m.Files == nil {
		return m, fmt.Errorf("unsupported managed manifest")
	}
	return m, nil
}
func privateMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0700
	}
	return 0600
}

// Current compares desired inputs with the applied manifest, not live files a
// running harness may be changing. It distinguishes file deferral from changes
// to runtime-only hooks or launch settings without adding another fingerprint.
func Current(manifestPath string, desired map[string]artifact.File, merges []harness.Merge) (bool, error) {
	if err := Validate(desired, merges); err != nil {
		return false, err
	}
	m, err := loadManifest(manifestPath)
	if err != nil {
		return false, err
	}
	if len(m.Files) != len(desired) {
		return false, nil
	}
	rules := map[string]harness.Merge{}
	for _, r := range merges {
		rules[r.Path] = r
	}
	for p, f := range desired {
		e, ok := m.Files[p]
		if !ok || e.Conflict {
			return false, nil
		}
		if r, structured := rules[p]; structured {
			if e.Strategy != "json-keys" || len(e.Keys) != len(r.Keys) {
				return false, nil
			}
			obj, err := object(f.Data)
			if err != nil {
				return false, err
			}
			keys := map[string]bool{}
			for _, k := range e.Keys {
				keys[k] = true
			}
			for _, k := range r.Keys {
				if !keys[k] {
					return false, nil
				}
				value, present := obj[k]
				recorded, applied := e.KeyHashes[k]
				if present != applied || (present && hash(value) != recorded) {
					return false, nil
				}
			}
		} else if e.Strategy != "file" || e.Hash != hash(f.Data) || e.Mode != privateMode(f.Mode) {
			return false, nil
		}
	}
	return true, nil
}

func Sync(root, manifestPath, store string, desired map[string]artifact.File, merges []harness.Merge) error {
	if err := Validate(desired, merges); err != nil {
		return err
	}
	rules := map[string]harness.Merge{}
	for _, r := range merges {
		rules[r.Path] = r
	}
	for p := range desired {
		if _, err := fsutil.Path(root, p); err != nil {
			return err
		}
	}
	old, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	next := Manifest{Version: 1, Files: map[string]Entry{}}
	paths := map[string]bool{}
	for p := range desired {
		paths[p] = true
	}
	for p := range old.Files {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	conflicts := &Conflicts{}
	for _, p := range sorted {
		dst, err := fsutil.Path(root, p)
		if err != nil {
			return err
		}
		live, err := os.ReadFile(dst)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		var liveMode os.FileMode
		if exists {
			info, err := os.Stat(dst)
			if err != nil {
				return err
			}
			liveMode = info.Mode().Perm()
		}
		f, wanted := desired[p]
		mode := privateMode(f.Mode)
		previous, managed := old.Files[p]
		entry := Entry{Source: f.Source, Layer: f.Layer, Store: store, Path: p, Strategy: "file", Mode: mode, AppliedAt: time.Now().UTC()}
		rule, structured := rules[p]
		if !wanted && managed && previous.Strategy == "json-keys" {
			rule = harness.Merge{Keys: previous.Keys}
			structured = true
		}
		conflict := false
		if structured {
			entry.Strategy = "json-keys"
			entry.Keys = append([]string(nil), rule.Keys...)
			entry.KeyHashes = map[string]string{}
			current := map[string]json.RawMessage{}
			if exists {
				current, err = object(live)
				if err != nil {
					conflict = true
				}
			}
			if !conflict {
				obj := map[string]json.RawMessage{}
				if wanted {
					obj, err = object(f.Data)
					if err != nil {
						return err
					}
				}
				for _, k := range rule.Keys {
					if value, ok := obj[k]; ok {
						current[k] = value
						entry.KeyHashes[k] = hash(value)
					} else {
						delete(current, k)
					}
				}
				if wanted || exists {
					data, err := json.MarshalIndent(current, "", "  ")
					if err != nil {
						return err
					}
					if _, err = fsutil.Dir(root, filepath.Dir(p), 0700); err != nil {
						return err
					}
					if exists {
						mode = liveMode
					}
					entry.Mode = mode
					if err = fsutil.Write(dst, append(data, '\n'), mode); err != nil {
						return err
					}
				}
			}
		} else if wanted {
			if !exists || (managed && previous.Strategy == "file" && hash(live) == previous.Hash && liveMode == previous.Mode) {
				if _, err = fsutil.Dir(root, filepath.Dir(p), 0700); err != nil {
					return err
				}
				if err = fsutil.Write(dst, f.Data, mode); err != nil {
					return err
				}
				entry.Hash = hash(f.Data)
			} else {
				conflict = true
			}
		} else if exists {
			if previous.Strategy == "file" && hash(live) == previous.Hash && liveMode == previous.Mode {
				if err = os.Remove(dst); err != nil {
					return err
				}
			} else {
				conflict = true
			}
		}
		if conflict {
			if managed {
				entry = previous
			} else {
				entry.AppliedAt = time.Time{}
				entry.Mode = 0
			}
			entry.Conflict = true
			conflicts.Paths = append(conflicts.Paths, p)
		}
		if wanted || conflict {
			next.Files[p] = entry
		}
	}
	if err = fsutil.JSON(manifestPath, next); err != nil {
		return err
	}
	if len(conflicts.Paths) > 0 {
		return commanderror.New("managed_config_conflict", conflicts.Error(), root, conflicts)
	}
	return nil
}
