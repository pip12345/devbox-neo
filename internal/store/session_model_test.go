package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/fsutil"
)

func TestCompleteSessionIDAndExclusiveDirectoryClaim(t *testing.T) {
	id, err := fsutil.SessionID()
	if err != nil || !idPattern.MatchString(id) || len(id) != 12 {
		t.Fatal(id, err)
	}
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.Lock(context.Background(), id, id)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.Claim(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(s.Home, "sessions", id, "marker")
	if err := os.WriteFile(marker, []byte("original allocation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := lock.Claim(); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing directory was adopted", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original allocation" {
		t.Fatal("existing state changed", err)
	}
	if _, err := s.Lock(context.Background(), "a-name-not-an-id", id); err == nil {
		t.Fatal("storage alias accepted")
	}
}
