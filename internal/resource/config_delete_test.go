package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/store"
)

func TestDeleteConfigOnlyRemovesNamedHomeDirectory(t *testing.T) {
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Home: state.Home}
	owner, err := s.ConfigDirectory("base", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfig(context.Background(), owner, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "setup.sh"), []byte("keep?"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(state.Home, "configs", "link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "./base", ".", "~", outside, "link"} {
		if _, err := s.DeleteConfig(context.Background(), name); err == nil {
			t.Fatal("deleted non-named path or followed a symlink", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("deleted outside the selected home", err)
	}
	result, err := s.DeleteConfig(context.Background(), "base")
	if err != nil || result.Path != owner.Root || len(result.Deleted) != 1 || result.Deleted[0] != owner.Root {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(owner.Root); !os.IsNotExist(err) {
		t.Fatal("config directory or optional file survived deletion", err)
	}
	if _, err := s.DeleteConfig(context.Background(), "base"); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing named config did not report absence", err)
	}
}

func TestDeleteConfigAllowsIncompleteDirectoryButNotUnreadableSessions(t *testing.T) {
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Home: state.Home}
	path := filepath.Join(state.Home, "configs", "incomplete")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(state.Home, "sessions", "devbox-broken")
	if err := os.Mkdir(bad, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteConfig(context.Background(), "incomplete"); err == nil || !strings.Contains(err.Error(), "Cannot verify every saved session") {
		t.Fatal("invalid session state was ignored", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("blocked deletion removed config", err)
	}
	if err := os.RemoveAll(bad); err != nil {
		t.Fatal(err)
	}
	result, err := s.DeleteConfig(context.Background(), "incomplete")
	if err != nil || len(result.Deleted) != 1 {
		t.Fatal("could not remove incomplete config", result, err)
	}
	var actionable *commanderror.Error
	if _, err := s.DeleteConfig(context.Background(), "incomplete"); !errors.As(err, &actionable) || actionable.Code != "config_missing" {
		t.Fatal("missing config did not use actionable error", err)
	}
}
