package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
)

func TestStoppedAccessAllowsExistingAttachment(t *testing.T) {
	for _, action := range []string{"open", "shell", "exec", "start"} {
		t.Run(action, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, made.SessionID)
			lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := lock.Lease("open")
			lock.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := accessAction(ctx, e, q, r.Directory, action); err != nil {
				t.Fatal("attachment vetoed ordinary startup", err)
			}
			c, exists := sessionSnapshot(t, e, r.ID)
			if !exists || !c.State.Running {
				t.Fatal("access did not start the recorded container")
			}
			lock, err = e.Store.Lock(ctx, r.Directory, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			active, err := lock.LiveLeases()
			if err != nil || len(active) != 1 || active[0].ID != lease.ID {
				t.Fatal("access lost the existing attachment", active, err)
			}
			if _, err := lock.Release(lease.ID); err != nil {
				t.Fatal(err)
			}
			if err := e.stopUnattached(lock, sessionRecord(t, e, r.ID)); err != nil {
				t.Fatal(err)
			}
			c, _ = sessionSnapshot(t, e, r.ID)
			if c.State.Running != (action == "start") {
				t.Fatal("last attachment changed automatic/manual lifetime")
			}
		})
	}
}

func TestForcedRecreationRetiresOnlyOldAttachments(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "manual"}[manual], func(t *testing.T) {
			e, d, q := fixture(t)
			e.Streams = docker.Streams{}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			old := sessionRecord(t, e, made.SessionID)
			q.Workspace, q.LocalName = old.Directory, ""
			if manual {
				if _, err := e.Start(ctx, old.Directory, ""); err != nil {
					t.Fatal(err)
				}
			}
			launch := append([]string{old.Applied.Launch.Binary}, old.Applied.Launch.Args...)
			arrived := make(chan struct{}, 2)
			oldExit, newExit := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			d.Attached = func(ctx context.Context, c docker.Command) error {
				if !argvSuffix(c.Args, launch) {
					return nil
				}
				exit := oldExit
				if calls.Add(1) > 1 {
					exit = newExit
				}
				arrived <- struct{}{}
				select {
				case <-exit:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			open := func() <-chan error {
				done := make(chan error, 1)
				go func() { _, err := e.Open(ctx, openRequest(q)); done <- err }()
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal("attachment did not start", ctx.Err())
				}
				return done
			}
			oldDone := open()
			var busy *commanderror.Error
			if _, err := e.Recreate(ctx, recreateRequest(q), false); !errors.As(err, &busy) || busy.Code != "session_busy" || busy.Target != old.Directory || !strings.Contains(busy.Message, "host PID") || !reflect.DeepEqual(busy.Next[0].Command, []string{"dbx", "status", old.Directory}) {
				t.Fatal("ordinary recreation lost attachment protection or actionable details", err)
			}
			forced := recreateRequest(q)
			forced.Options.Force = true
			if _, err := e.Recreate(ctx, forced, false); err != nil {
				t.Fatal(err)
			}
			current := sessionRecord(t, e, old.ID)
			if current.ID != old.ID || current.Directory != old.Directory || current.Applied.SetupContainer == old.Applied.SetupContainer {
				t.Fatal("forced recreation changed identity or retained the old container")
			}
			c, _ := sessionSnapshot(t, e, old.ID)
			if c.State.Running != manual {
				t.Fatal("interrupted automatic attachments left an unattended container running")
			}
			newDone := open()
			before := sessionRecord(t, e, old.ID)
			close(oldExit)
			if err := <-oldDone; err != nil {
				t.Fatal(err)
			}
			if after := sessionRecord(t, e, old.ID); !reflect.DeepEqual(before, after) {
				t.Fatal("retired cleanup changed replacement state")
			}
			c, _ = sessionSnapshot(t, e, old.ID)
			if !c.State.Running {
				t.Fatal("retired cleanup stopped a new attachment")
			}
			close(newExit)
			if err := <-newDone; err != nil {
				t.Fatal(err)
			}
			c, _ = sessionSnapshot(t, e, old.ID)
			if c.State.Running != manual {
				t.Fatal("last new attachment did not preserve automatic/manual shutdown")
			}
		})
	}
}

func TestForcedRecreationFailureRetainsAttachmentProtection(t *testing.T) {
	for _, failure := range []string{"config", "build", "remove"} {
		t.Run(failure, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, made.SessionID)
			lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := lock.Lease("open")
			lock.Close()
			if err != nil {
				t.Fatal(err)
			}
			forced := recreateRequest(q)
			forced.Options.Force = true
			if failure == "config" {
				write(t, filepath.Join(q.Sources[0].Path, "config.json"), "broken")
			} else {
				d.Fail = func(args []string) error {
					if (failure == "build" && args[0] == "build") || (failure == "remove" && args[0] == "rm") {
						return errors.New("injected failure")
					}
					return nil
				}
			}
			if _, err := e.Recreate(ctx, forced, failure == "build"); err == nil {
				t.Fatal("injected failure did not fail recreation")
			}
			lock, err = e.Store.Lock(ctx, r.Directory, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			active, err := lock.LiveLeases()
			if err != nil || len(active) != 1 || active[0].ID != lease.ID {
				t.Fatal("failed replacement prematurely retired attachments", active, err)
			}
			if current := sessionRecord(t, e, r.ID); !reflect.DeepEqual(current, r) {
				t.Fatal("failed replacement published new state")
			}
			if _, exists := sessionSnapshot(t, e, r.ID); !exists {
				t.Fatal("failed pre-removal replacement lost the old container")
			}
		})
	}
}
