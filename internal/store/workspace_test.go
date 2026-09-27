package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/fsutil"
)

func testDefault(t *testing.T, s *Store, workspace string, selected *DefaultSession) {
	t.Helper()
	lock, err := s.lockWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.close()
	if err := lock.set(selected); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceDefaultPersistenceAndAbsentReads(t *testing.T) {
	ctx := context.Background()
	s := &Store{Home: t.TempDir()}
	workspace := "/missing/workspace"
	key, err := WorkspaceKey(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(workspace))
	if key != hex.EncodeToString(sum[:]) {
		t.Fatal("workspace key is not the hash of the canonical path bytes")
	}
	if selected, err := s.ReadDefault(ctx, workspace); err != nil || selected != nil {
		t.Fatalf("absent default: %+v %v", selected, err)
	}
	if err := s.ClearDefault(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Home, "state/workspaces")); !os.IsNotExist(err) {
		t.Fatalf("read or absent clear seeded workspace state: %v", err)
	}
	want := &DefaultSession{ID: strings.Repeat("a", 32)}
	testDefault(t, s, workspace, want)
	got, err := s.ReadDefault(ctx, workspace)
	if err != nil || got == nil || *got != *want {
		t.Fatalf("saved default: %+v %v", got, err)
	}
	for path, mode := range map[string]os.FileMode{
		"state/workspaces":                        0700,
		"state/workspaces/" + key + ".json":       0600,
		"state/locks/workspaces":                  0700,
		"state/locks/workspaces/" + key + ".lock": 0600,
	} {
		info, err := os.Stat(filepath.Join(s.Home, path))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions for %s: %v %v", path, info, err)
		}
	}
	if err := s.ClearDefault(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Home, "state/workspaces", key+".json"))
	if err != nil || !strings.Contains(string(data), `"default_session": null`) {
		t.Fatalf("cleared record: %s %v", data, err)
	}
	if selected, err := s.ReadDefault(ctx, workspace); err != nil || selected != nil {
		t.Fatalf("cleared default: %+v %v", selected, err)
	}
}

func TestWorkspaceDefaultCorruptionIsNotAbsence(t *testing.T) {
	ctx := context.Background()
	workspace := "/workspace"
	for _, contents := range []string{
		`{`,
		`null`,
		`{"version":1,"workspace":"/workspace","default_session":null}`,
		`{"version":1,"workspace":"/other","default_session":null}`,
		`{"version":1,"workspace":"/workspace"}`,
		`{"version":1,"workspace":"/workspace","default_session":{"name":"devbox-name","id":"bad"}}`,
		`{"version":1,"workspace":"/workspace","default_session":null,"extra":true}`,
	} {
		t.Run(contents, func(t *testing.T) {
			s := &Store{Home: t.TempDir()}
			key, _ := WorkspaceKey(workspace)
			dir, err := fsutil.Dir(s.Home, "state/workspaces", 0700)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, key+".json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadDefault(ctx, workspace); err == nil {
				t.Fatal("corrupt default treated as absence")
			}
			if err := s.ClearDefault(ctx, workspace); err == nil {
				t.Fatal("clear silently replaced corrupt state")
			}
			data, _ := os.ReadFile(path)
			if string(data) != contents {
				t.Fatal("failed operation changed corrupt state")
			}
		})
	}
}

func TestWorkspaceDefaultClearRequiresMatchingID(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := "/missing/workspace"
	selected := DefaultSession{ID: strings.Repeat("a", 32)}
	testDefault(t, s, workspace, &selected)
	lock, err := s.Lock(ctx, "session-directory", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.ClearMatchingDefault(ctx, workspace, strings.Repeat("b", 32)); err == nil {
		t.Fatal("wrong ID bypassed the operation lock")
	}
	got, err := s.ReadDefault(ctx, workspace)
	if err != nil || got == nil || *got != selected {
		t.Fatalf("different session ID cleared selection: %+v %v", got, err)
	}
	// No session record or workspace directory exists. Committed move retries
	// must still be able to clear a default using the journal's saved identity.
	if err := lock.ClearMatchingDefault(ctx, workspace, selected.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ReadDefault(ctx, workspace); err != nil || got != nil {
		t.Fatalf("matching default was not cleared: %+v %v", got, err)
	}
	lock.Close()
	if err := lock.ClearMatchingDefault(ctx, workspace, selected.ID); err == nil {
		t.Fatal("allowed cleanup without the session operation lock")
	}
}

func TestWorkspaceLocksRemainExternalAndRespectCancellation(t *testing.T) {
	ctx := context.Background()
	s := &Store{Home: t.TempDir()}
	workspace := "/workspace"
	lock, err := s.lockWorkspace(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.close()
	if err := lock.set(&DefaultSession{ID: strings.Repeat("a", 32)}); err != nil {
		t.Fatal(err)
	}
	path, _ := lock.path()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := s.lockWorkspace(wait, workspace); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("state deletion released external lock: %v", err)
	}
	lock.close()
	if err := lock.set(nil); err == nil {
		t.Fatal("workspace mutation after lock release")
	}
	if _, err := WorkspaceKey("relative"); err == nil {
		t.Fatal("accepted relative workspace identity")
	}
}
