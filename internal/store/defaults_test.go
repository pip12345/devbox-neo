package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/fsutil"
)

func testDefault(t *testing.T, s *Store, workspace string, selected *DefaultSession) {
	t.Helper()
	lock, err := s.LockDefaults(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	id := ""
	if selected != nil {
		id = selected.ID
	}
	if err := lock.set(workspace, id); err != nil {
		t.Fatal(err)
	}
}

func TestFolderDefaultsPersistenceAndAbsentReads(t *testing.T) {
	ctx := context.Background()
	s := &Store{Home: t.TempDir()}
	workspace := "/missing/workspace"
	path := filepath.Join(s.Home, "state/folder-defaults.json")
	if selected, err := s.ReadDefault(ctx, workspace); err != nil || selected != nil {
		t.Fatal(selected, err)
	}
	if err := s.ClearDefault(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("absent read/clear seeded defaults", err)
	}
	want := &DefaultSession{ID: strings.Repeat("a", 32)}
	testDefault(t, s, workspace, want)
	testDefault(t, s, "/other", &DefaultSession{ID: strings.Repeat("b", 32)})
	if got, err := s.ReadDefault(ctx, workspace); err != nil || got == nil || *got != *want {
		t.Fatal(got, err)
	}
	for _, rel := range []string{"state/folder-defaults.json", "state/locks/folder-defaults.lock"} {
		if info, err := os.Stat(filepath.Join(s.Home, rel)); err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(rel, info, err)
		}
	}
	if err := s.ClearDefault(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state FolderDefaults
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Defaults) != 1 || state.Defaults["/other"] != strings.Repeat("b", 32) || strings.Contains(string(data), "null") {
		t.Fatal("clear removed another choice or retained an empty entry", string(data))
	}
	if selected, err := s.ReadDefault(ctx, workspace); err != nil || selected != nil {
		t.Fatal(selected, err)
	}
	if err := s.ClearDefault(ctx, "/other"); err != nil {
		t.Fatal(err)
	}
	lock, err := s.LockDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	state, err = lock.Read()
	if err != nil || len(state.Defaults) != 0 {
		t.Fatal(state, err)
	}
}

func TestFolderDefaultsCorruptionIsNotAbsence(t *testing.T) {
	for _, contents := range []string{
		`{`, `null`, `{}`, `{"version":1}`, `{"version":2,"defaults":{}}`,
		`{"version":1,"defaults":null}`, `{"version":1,"defaults":{"/work":null}}`,
		`{"version":1,"defaults":{"/work":"bad"}}`,
		`{"version":1,"defaults":{"relative":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"version":1,"defaults":{"/a/../b":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"version":1,"defaults":{},"extra":true}`, `{"version":1,"defaults":{},"defaults":{}}`,
		`{"version":2,"workspace":"/workspace","default_session":null}`,
	} {
		t.Run(contents, func(t *testing.T) {
			s := &Store{Home: t.TempDir()}
			dir, err := fsutil.Dir(s.Home, "state", 0700)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "folder-defaults.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadDefault(context.Background(), "/workspace"); err == nil {
				t.Fatal("corrupt defaults accepted")
			}
			if err := s.ClearDefault(context.Background(), "/workspace"); err == nil {
				t.Fatal("clear replaced corrupt defaults")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != contents {
				t.Fatal("corrupt state modified", err)
			}
		})
	}
}

func TestFolderDefaultClearRequiresMatchingID(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := "/missing/workspace"
	id := strings.Repeat("a", 32)
	testDefault(t, s, workspace, &DefaultSession{ID: id})
	lock, err := s.Lock(ctx, "session-directory", id)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.ClearMatchingDefault(ctx, workspace, strings.Repeat("b", 32)); err == nil {
		t.Fatal("wrong session ID bypassed lock")
	}
	if err := lock.ClearMatchingDefault(ctx, workspace, id); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ReadDefault(ctx, workspace); err != nil || got != nil {
		t.Fatal(got, err)
	}
	// A newer selection in the same folder must survive cleanup of the old one.
	testDefault(t, s, workspace, &DefaultSession{ID: strings.Repeat("b", 32)})
	if err := lock.ClearMatchingDefault(ctx, workspace, id); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ReadDefault(ctx, workspace); err != nil || got == nil || got.ID != strings.Repeat("b", 32) {
		t.Fatal(got, err)
	}
	lock.Close()
	if err := lock.ClearMatchingDefault(ctx, workspace, id); err == nil {
		t.Fatal("cleanup without session lock")
	}
}

func TestDefaultsLockSurvivesDataDeletion(t *testing.T) {
	ctx := context.Background()
	s := &Store{Home: t.TempDir()}
	lock, err := s.LockDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.set("/workspace", strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	path, _ := lock.path()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := s.LockDefaults(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("data deletion bypassed external lock", err)
	}
	lock.Close()
	if err := lock.set("/workspace", ""); err == nil {
		t.Fatal("mutation after lock release")
	}
	if _, err := s.ReadDefault(ctx, "relative"); err == nil {
		t.Fatal("relative folder accepted")
	}
}

func TestConcurrentFolderSelectionsDoNotLoseUpdates(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	ctx := context.Background()
	results := make(chan error, 12)
	for i := range 12 {
		go func() {
			lock, err := s.LockDefaults(ctx)
			if err != nil {
				results <- err
				return
			}
			defer lock.Close()
			results <- lock.set(fmt.Sprintf("/folder-%d", i), fmt.Sprintf("%032x", i+1))
		}()
	}
	for range 12 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	for i := range 12 {
		got, err := s.ReadDefault(ctx, fmt.Sprintf("/folder-%d", i))
		if err != nil || got == nil || got.ID != fmt.Sprintf("%032x", i+1) {
			t.Fatal("lost concurrent selection", got, err)
		}
	}
}
