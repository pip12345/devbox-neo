package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/sshshare"
)

func fakeContainerMaster(t *testing.T, e *Engine, name string) func(context.Context, docker.Command) error {
	t.Helper()
	root := filepath.Join(e.Store.Home, "sessions", name, sshshare.RelativeRoot)
	return func(ctx context.Context, cmd docker.Command) error {
		i := slices.Index(cmd.Args, sshshare.Supervisor)
		if i < 0 {
			return nil
		}
		rel := strings.TrimPrefix(cmd.Args[i+2], sshshare.Mount+"/")
		dir, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		defer dir.Close()
		listener, err := net.Listen("unix", fmt.Sprintf("/proc/self/fd/%d/socket", dir.Fd()))
		if err != nil {
			return err
		}
		defer listener.Close()
		<-ctx.Done()
		return ctx.Err()
	}
}

func awaitSSH(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatal("SSH not connected")
	}
}
func finishSSH(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("SSH did not finish")
	}
}

func TestSSHStartupLeasesConcurrentConnectionsAndCleanup(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Attached = fakeContainerMaster(t, e, result.Name)
	connect := func(destination string) (context.CancelFunc, <-chan error) {
		cctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		ready := make(chan struct{})
		go func() {
			done <- e.SSH(cctx, result.Name, "", destination, SSHOptions{Connected: func(_, alias string) {
				if alias == "" {
					panic("missing alias")
				}
				close(ready)
			}})
		}()
		t.Cleanup(cancel)
		awaitSSH(t, ready)
		return cancel, done
	}
	cancelFirst, first := connect("staging")
	r := record(t, e, result.Name)
	if r.Action != "ssh" {
		t.Fatal(r.Action)
	}
	if err := e.Stop(ctx, result.Name, "", false); err == nil {
		t.Fatal("SSH lease did not protect stop")
	}
	if _, err := e.Recreate(ctx, q, false); err == nil {
		t.Fatal("SSH lease did not protect recreation")
	}
	if err := e.SSH(ctx, result.Name, "", "staging", SSHOptions{}); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatal("duplicate not refused", err)
	}
	// Running SSH access follows the same config-independent policy as exec.
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	cancelSecond, second := connect("other")
	cancelFirst()
	finishSSH(t, first)
	c, _ := d.Snapshot(result.Name)
	if !c.State.Running {
		t.Fatal("first connection stopped second")
	}
	cancelSecond()
	finishSSH(t, second)
	c, _ = d.Snapshot(result.Name)
	if c.State.Running {
		t.Fatal("last SSH lease did not trigger automatic shutdown")
	}
	entries, _ := filepath.Glob(filepath.Join(e.Store.Home, "sessions", result.Name, sshshare.RelativeRoot, "c", "*", "config"))
	if len(entries) != 0 {
		t.Fatal("dead connections still published", entries)
	}
	l, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	leases, err := l.Active()
	if err != nil || len(leases) != 0 {
		t.Fatal(leases, err)
	}
}

func TestSSHMissingMountNeedsExplicitRecreation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	r.Creation.Mounts = slices.DeleteFunc(r.Creation.Mounts, func(m docker.Mount) bool { return m.Target == sshshare.Mount })
	if err = l.Save(r); err != nil {
		t.Fatal(err)
	}
	l.Close()
	before := len(d.History())
	err = e.SSH(ctx, result.Name, "", "staging", SSHOptions{})
	var actionable *commanderror.Error
	if !errors.As(err, &actionable) || actionable.Code != "ssh_mount_missing" {
		t.Fatal(err)
	}
	if len(d.History()) != before {
		t.Fatal("missing mount triggered runtime mutation")
	}
}

func TestSSHInvalidConfigBlocksStoppedStartup(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := count(d, "start")
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	if err := e.SSH(ctx, result.Name, "", "staging", SSHOptions{}); err == nil {
		t.Fatal("invalid config ignored")
	}
	if count(d, "start") != before {
		t.Fatal("invalid config started container")
	}
}

func TestSSHHostAuthenticationFailureReleasesLeaseAndStops(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 255\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	err = e.SSH(ctx, result.Name, "", "staging", SSHOptions{HostMaster: true, Connected: func(_, _ string) { t.Error("published failed authentication") }})
	var exit *sshshare.ExitError
	if !errors.As(err, &exit) || exit.Code != 255 {
		t.Fatal(err)
	}
	c, _ := d.Snapshot(result.Name)
	if c.State.Running {
		t.Fatal("failed login left startup running")
	}
}

func TestSSHForcedStopEndsConnection(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Attached = fakeContainerMaster(t, e, result.Name)
	ready := make(chan struct{})
	done := make(chan error, 1)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	go func() {
		done <- e.SSH(cctx, result.Name, "", "staging", SSHOptions{Connected: func(_, _ string) { close(ready) }})
	}()
	awaitSSH(t, ready)
	if err := e.Stop(ctx, result.Name, "", true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "no longer available") {
			t.Fatal(err)
		}
	case <-cctx.Done():
		t.Fatal("forced stop left master alive")
	}
}
