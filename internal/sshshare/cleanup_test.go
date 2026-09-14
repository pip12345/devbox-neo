package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestCancellationStopsHostMasterBeforeReturning(t *testing.T) {
	fakeSSH(t)
	c, err := Prepare(t.TempDir(), "staging")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunHost(ctx, c, nil, io.Discard, io.Discard) }()
	eventually(t, c.Ready)
	b, err := os.ReadFile(filepath.Join(c.Root, c.Relative, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host cancellation hung")
	}
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("SSH child survived host cancellation")
	}
}

func TestConfigRemovalFailureStillReleasesController(t *testing.T) {
	c, err := Prepare(t.TempDir(), "staging")
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unrelated, filepath.Join(c.Root, c.Relative, "config")); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err == nil {
		t.Fatal("unsafe config path ignored")
	}
	file, err := os.OpenFile(filepath.Join(c.Root, c.Relative, "owner.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	held, err := lockHeld(file)
	if err != nil || held {
		t.Fatal("controller lock leaked", held, err)
	}
	if b, err := os.ReadFile(unrelated); err != nil || string(b) != "keep" {
		t.Fatal("followed config symlink", err)
	}
}
