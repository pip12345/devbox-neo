package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func oldDefault(t *testing.T, home, folder, id string) string {
	t.Helper()
	var selected *store.DefaultSession
	if id != "" {
		selected = &store.DefaultSession{ID: id}
	}
	dir, err := fsutil.Dir(home, "state/workspaces", 0700)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(folder))
	path := filepath.Join(dir, hex.EncodeToString(hash[:])+".json")
	if err := fsutil.JSON(path, struct {
		Version   int                   `json:"version"`
		Workspace string                `json:"workspace"`
		Default   *store.DefaultSession `json:"default_session"`
	}{2, folder, selected}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsMigrationPreservesChoicesWithoutDockerOrSessions(t *testing.T) {
	home := t.TempDir()
	first := oldDefault(t, home, "/removed/folder", strings.Repeat("a", 32))
	oldDefault(t, home, "/another", strings.Repeat("b", 32))
	oldDefault(t, home, "/cleared", "")
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	required, err := Pending(ctx, home)
	if err != nil || required == nil || !strings.Contains(required.Description, "Containers, session data and history are unchanged") {
		t.Fatal(required, err)
	}
	if after, err := os.ReadFile(first); err != nil || !bytes.Equal(before, after) {
		t.Fatal("detection modified defaults", err)
	}
	if _, err := os.Stat(filepath.Join(home, "state/locks")); !os.IsNotExist(err) {
		t.Fatal("detection created locks", err)
	}
	d := &dockertest.Daemon{}
	if err := required.Apply(ctx, docker.Runtime{Runner: d}); err != nil {
		t.Fatal(err)
	}
	if len(d.History()) != 0 {
		t.Fatal("preference migration touched Docker")
	}
	s := &store.Store{Home: home}
	for folder, id := range map[string]string{"/removed/folder": strings.Repeat("a", 32), "/another": strings.Repeat("b", 32)} {
		got, err := s.ReadDefault(ctx, folder)
		if err != nil || got == nil || got.ID != id {
			t.Fatal("selection lost", folder, got, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "state/folder-defaults.json"))
	if err != nil || strings.Contains(string(data), "/cleared") || strings.Contains(string(data), "null") {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(filepath.Join(home, "state/workspaces")); !os.IsNotExist(err) {
		t.Fatal("old files remain", err)
	}
	if required, err := Pending(ctx, home); err != nil || required != nil {
		t.Fatal(required, err)
	}
}

func TestDefaultsMigrationRetriesPublishedMappingWithoutReplacingChoices(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial cleanup", true: "conflict"}[conflict], func(t *testing.T) {
			home := t.TempDir()
			id := strings.Repeat("a", 32)
			old := oldDefault(t, home, "/remaining", id)
			s := &store.Store{Home: home}
			lock, err := s.LockDefaults(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if conflict {
				id = strings.Repeat("b", 32)
			}
			if err := lock.Save(store.FolderDefaults{Version: 1, Defaults: map[string]string{"/remaining": id, "/already-converted": strings.Repeat("c", 32)}}); err != nil {
				t.Fatal(err)
			}
			lock.Close()
			path := filepath.Join(home, "state/folder-defaults.json")
			before, _ := os.ReadFile(path)
			err = migrateFolderDefaults(context.Background(), home)
			if conflict {
				if err == nil {
					t.Fatal("conflicting choice replaced")
				}
				if _, err := os.Stat(old); err != nil {
					t.Fatal("conflicting old choice removed", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
				t.Fatal("published choices changed on retry", err)
			}
		})
	}
}

func TestDefaultsMigrationPreflightsAllOldRecords(t *testing.T) {
	for _, invalid := range []string{
		`broken`, `{"version":2,"workspace":"/bad"}`, `{"version":1,"workspace":"/bad","default_session":null}`,
		`{"version":2,"workspace":"/wrong","default_session":null}`,
		`{"version":2,"workspace":"/bad","default_session":{"id":"invalid"}}`,
		`{"version":2,"workspace":"/bad","default_session":null,"extra":true}`,
		`{"version":2,"version":2,"workspace":"/bad","default_session":null}`,
	} {
		t.Run(invalid, func(t *testing.T) {
			home := t.TempDir()
			good := oldDefault(t, home, "/good", strings.Repeat("a", 32))
			bad := oldDefault(t, home, "/bad", "")
			if err := os.WriteFile(bad, []byte(invalid), 0600); err != nil {
				t.Fatal(err)
			}
			if err := migrateFolderDefaults(context.Background(), home); err == nil {
				t.Fatal("invalid source accepted")
			}
			if _, err := os.Stat(good); err != nil {
				t.Fatal("preflight removed valid source", err)
			}
			if _, err := os.Stat(filepath.Join(home, "state/folder-defaults.json")); !os.IsNotExist(err) {
				t.Fatal("preflight published partial mapping", err)
			}
		})
	}
}

func TestDefaultsMigrationCancellationAndSymlinksDoNotMutate(t *testing.T) {
	for _, mode := range []string{"cancel", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			path := oldDefault(t, home, "/folder", strings.Repeat("a", 32))
			ctx := context.Background()
			if mode == "cancel" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			} else {
				outside := filepath.Join(t.TempDir(), "saved-default")
				if err := os.Rename(path, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := migrateFolderDefaults(ctx, home); err == nil {
				t.Fatal("unsafe migration accepted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("source removed", err)
			}
			if _, err := os.Stat(filepath.Join(home, "state/folder-defaults.json")); !os.IsNotExist(err) {
				t.Fatal("mapping published", err)
			}
		})
	}
}

func TestEmptyOldDefaultsDirectoryCanBeMigrated(t *testing.T) {
	home := t.TempDir()
	if _, err := fsutil.Dir(home, "state/workspaces", 0700); err != nil {
		t.Fatal(err)
	}
	if err := migrateFolderDefaults(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	if pending, err := Pending(context.Background(), home); err != nil || pending != nil {
		t.Fatal(pending, err)
	}
}
