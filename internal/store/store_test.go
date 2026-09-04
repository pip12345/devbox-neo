package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devbox/internal/environment"
	"devbox/internal/fsutil"
)

func TestHomeAndExternalLocks(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(ctx, s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if s.Installation != again.Installation {
		t.Fatal("installation identity changed")
	}
	entries, _ := os.ReadDir(filepath.Join(s.Home, "profiles"))
	if len(entries) != 0 {
		t.Fatal("fresh home seeded profiles")
	}
	id, err := environment.Identify(t.TempDir(), "test", false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Lock(ctx, id.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Dir("."); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(s.Home, "sessions", id.Name)); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err = s.Lock(wait, id.Name); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("session deletion unlinked active lock: %v", err)
	}
	if _, err = l.Path("../../escape"); err == nil {
		t.Fatal("session-relative escape accepted")
	}
	l.Close()
	if _, err = l.Path("session.json"); err == nil {
		t.Fatal("mutation after lock release")
	}
}
func TestLeasePIDReuseAndCorruption(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := environment.Identify(t.TempDir(), "test", false)
	l, err := s.Lock(context.Background(), id.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lease, err := l.Lease("open", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if err = l.RequireIdle(); err == nil {
		t.Fatal("live lease ignored")
	}
	lease.Process.Start = "0"
	p, _ := l.Path(filepath.Join("active", lease.ID+".json"))
	if err = fsutil.JSON(p, lease); err != nil {
		t.Fatal(err)
	}
	active, err := l.Active()
	if err != nil || len(active) != 0 {
		t.Fatalf("PID reuse not reaped: %v", err)
	}
	lease, err = l.Lease("open", "stop")
	if err != nil {
		t.Fatal(err)
	}
	p, _ = l.Path(filepath.Join("active", lease.ID+".json"))
	os.WriteFile(p, []byte("bad"), 0600)
	if _, err = l.Active(); err == nil {
		t.Fatal("corrupt lease treated as absence")
	}
	if _, err = os.Stat(p); err != nil {
		t.Fatal("corrupt lease deleted")
	}
}
func TestCorruptIdentityNotReinitialized(t *testing.T) {
	home := t.TempDir()
	s, err := Open(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "state/installation-id"), []byte("bad"), 0600)
	if _, err = Open(context.Background(), home); err == nil {
		t.Fatal("corrupt ID replaced")
	}
	if s.Installation == "" {
		t.Fatal("empty original ID")
	}
}
