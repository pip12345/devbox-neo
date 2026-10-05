package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

// The fake models Docker's policy boundary, not a host reboot. Live-daemon
// restart acceptance remains a separate opt-in check.
func rebootDaemon(t *testing.T, d *dockertest.Daemon, name string) {
	t.Helper()
	c, exists := d.Snapshot(name)
	if !exists {
		t.Fatal("reboot fixture container is missing", name)
	}
	c.State.Running = c.HostConfig.RestartPolicy.Name == "unless-stopped"
	d.SetContainer(c)
}

func TestManualStartStopAndRebootPolicy(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check := func(running, manual bool, policy string) {
		t.Helper()
		c, _ := sessionSnapshot(t, e, made.SessionID)
		r := sessionRecord(t, e, made.SessionID)
		if c.State.Running != running || r.Settings.ManualStart != manual || c.HostConfig.RestartPolicy.Name != policy {
			t.Fatalf("running=%v manual=%v policy=%s", c.State.Running, r.Settings.ManualStart, c.HostConfig.RestartPolicy.Name)
		}
	}
	check(false, false, "no")
	if _, err = e.Open(ctx, openRequest(q)); err != nil {
		t.Fatal(err)
	}
	check(false, false, "no")
	if _, err = e.Start(ctx, made.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	check(true, true, "unless-stopped")
	if _, err = e.Open(ctx, openRequest(q)); err != nil {
		t.Fatal(err)
	}
	if err = e.Exec(ctx, made.SessionID, "", []string{"true"}, false); err != nil {
		t.Fatal(err)
	}
	check(true, true, "unless-stopped")
	rebootDaemon(t, d, sessionRecord(t, e, made.SessionID).Applied.Creation.Name)
	check(true, true, "unless-stopped")
	if _, err = e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	check(true, true, "unless-stopped")
	forgetSession(t, e, made.SessionID)
	if _, err = e.Open(ctx, openRequest(q)); err == nil {
		t.Fatal("open rebuilt missing runtime")
	}
	if !sessionRecord(t, e, made.SessionID).Settings.ManualStart {
		t.Fatal("failed access cleared keep-running intent")
	}
	if _, err = e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	check(true, true, "unless-stopped")
	d.Fail = func(args []string) error {
		if args[0] == "stop" {
			return errors.New("stop failed")
		}
		return nil
	}
	if err = e.Stop(ctx, made.SessionID, "", false); err == nil {
		t.Fatal("failed stop succeeded")
	}
	check(true, true, "unless-stopped")
	d.Fail = nil
	if err = e.Stop(ctx, made.SessionID, "", false); err != nil {
		t.Fatal(err)
	}
	check(false, false, "no")
	rebootDaemon(t, d, sessionRecord(t, e, made.SessionID).Applied.Creation.Name)
	check(false, false, "no")
	if _, err = e.Open(ctx, openRequest(q)); err != nil {
		t.Fatal(err)
	}
	check(false, false, "no")
}

func TestManualStartDuringConcurrentAttachmentsWinsRegardlessOfExitOrder(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "manual"}[manual], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			e.Streams = docker.Streams{}
			r := sessionRecord(t, e, made.SessionID)
			argv := append([]string{r.Applied.Launch.Binary}, r.Applied.Launch.Args...)
			arrived := make(chan struct{}, 2)
			release := make(chan struct{})
			d.Attached = func(ctx context.Context, c docker.Command) error {
				if !argvSuffix(c.Args, argv) {
					return nil
				}
				arrived <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); _, err := e.Open(ctx, openRequest(q)); errs <- err }()
			}
			for range 2 {
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			c, _ := sessionSnapshot(t, e, made.SessionID)
			if !c.State.Running || sessionRecord(t, e, made.SessionID).Settings.ManualStart {
				t.Fatal("open changed manual intent")
			}
			if manual {
				// A fresh Engine represents a separate CLI process using the same record.
				other := *e
				if _, err = other.Start(ctx, made.SessionID, ""); err != nil {
					t.Fatal(err)
				}
				if err = other.Stop(ctx, made.SessionID, "", false); err == nil {
					t.Fatal("stop ignored live attachments")
				}
				if !sessionRecord(t, e, made.SessionID).Settings.ManualStart {
					t.Fatal("rejected stop cleared manual intent")
				}
			}
			close(release)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			c, _ = sessionSnapshot(t, e, made.SessionID)
			if c.State.Running != manual {
				t.Fatal("last attachment applied the wrong lifetime", c.State)
			}
			rebootDaemon(t, d, sessionRecord(t, e, made.SessionID).Applied.Creation.Name)
			c, _ = sessionSnapshot(t, e, made.SessionID)
			if c.State.Running != manual {
				t.Fatal("wrong reboot policy")
			}
		})
	}
}

func TestRestartPolicyIsManagedAndStartFailureDoesNotClaimManualIntent(t *testing.T) {
	if err := docker.ValidateRaw([]string{"--restart=always"}, nil, "", ""); err == nil {
		t.Fatal("raw Docker option overrode manual intent")
	}
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Fail = func(args []string) error {
		if args[0] == "update" {
			return errors.New("update failed")
		}
		return nil
	}
	if _, err = e.Start(ctx, made.SessionID, ""); err == nil {
		t.Fatal("restart policy failure was ignored")
	}
	if sessionRecord(t, e, made.SessionID).Settings.ManualStart {
		t.Fatal("failed start recorded manual intent")
	}
}
