package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/fsutil"
)

func TestIncompleteDirectoryRejectsRecordsAndUnsafeRoots(t *testing.T) {
	for _, kind := range []string{"record", "record symlink", "root symlink", "file", "missing", "escape"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, err := Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			name := "dbx-test.main"
			root := filepath.Join(s.Home, "sessions", name)
			if kind != "missing" && kind != "escape" && kind != "root symlink" && kind != "file" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "record":
				err = os.WriteFile(filepath.Join(root, "session.json"), []byte("corrupt"), 0600)
			case "record symlink":
				err = os.Symlink(filepath.Join(t.TempDir(), "absent"), filepath.Join(root, "session.json"))
			case "root symlink":
				err = os.Symlink(t.TempDir(), root)
			case "file":
				err = os.WriteFile(root, []byte("not a directory"), 0600)
			case "escape":
				name = "../outside"
			}
			if err != nil {
				t.Fatal(err)
			}
			directory, err := s.InspectIncompleteDirectory(ctx, name)
			if directory != nil {
				t.Fatal("unsafe root classified as incomplete", directory)
			}
			wantError := kind == "root symlink" || kind == "file" || kind == "escape"
			if (err != nil) != wantError {
				t.Fatal(err)
			}
		})
	}
}

func TestIncompleteDirectoryRemovalLeavesExternalState(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "dbx-test.main"
	root := filepath.Join(s.Home, "sessions", name)
	outside := filepath.Join(t.TempDir(), "keep")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(s.Home, "state/locks/sessions", "existing.lock")
	if err := os.WriteFile(lockPath, []byte("keep lock"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := s.LockNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fsutil.Unlock(lock)
	directory, err := s.InspectIncompleteDirectory(ctx, name)
	if err != nil || directory == nil {
		t.Fatal(directory, err)
	}
	if err := directory.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("directory retained", err)
	}
	for _, path := range []string{outside, lockPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("deleted unrelated state", path, err)
		}
	}
}
