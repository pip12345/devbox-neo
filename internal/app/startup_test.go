package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func accessAction(ctx context.Context, e *Engine, q Request, name, action string) error {
	switch action {
	case "open":
		_, err := e.Open(ctx, q)
		return err
	case "start":
		_, err := e.Start(ctx, name, "")
		return err
	case "recreate":
		_, err := e.Recreate(ctx, q, false)
		return err
	default:
		return e.Exec(ctx, name, "", []string{"true"}, action == "shell")
	}
}

func TestAccessSynchronizesOnlyAtStartup(t *testing.T) {
	for _, action := range []string{"open", "start", "shell", "exec", "recreate"} {
		for _, running := range []bool{false, true} {
			t.Run(action+map[bool]string{false: "/stopped", true: "/running"}[running], func(t *testing.T) {
				e, d, q := fixture(t)
				ctx := context.Background()
				profile := filepath.Join(e.Store.Home, "profiles/test")
				write(t, filepath.Join(profile, "config.json"), `{"version":1,"harness":"pi"}`)
				write(t, filepath.Join(profile, "pi/managed.txt"), "old")
				write(t, filepath.Join(profile, "pi/obsolete.txt"), "old")
				write(t, filepath.Join(profile, "pi/settings.json"), `{"packages":["old"]}`)
				created, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				if running {
					if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
						t.Fatal(err)
					}
				}
				before := sessionRecord(t, e, created.SessionID)
				root := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, created.SessionID).Directory, "harnesses/pi/stores/home")
				write(t, filepath.Join(root, "managed.txt"), "local edit")
				write(t, filepath.Join(root, "obsolete.txt"), "local obsolete edit")
				write(t, filepath.Join(root, "settings.json"), `{"packages":["local"],"theme":"personal"}`)
				write(t, filepath.Join(root, "sessions/history.json"), "history")
				write(t, filepath.Join(root, "unmanaged.txt"), "keep")
				write(t, filepath.Join(profile, "pi/managed.txt"), "new")
				write(t, filepath.Join(profile, "pi/settings.json"), `{"packages":["new"]}`)
				if err := os.Remove(filepath.Join(profile, "pi/obsolete.txt")); err != nil {
					t.Fatal(err)
				}
				wantSync := !running || action == "recreate"
				starts := count(d, "start")
				d.Fail = func(args []string) error {
					if args[0] == "start" && string(getFile(t, filepath.Join(root, "managed.txt"))) != "new" {
						t.Fatal("container started before sync")
					}
					return nil
				}
				if err := accessAction(ctx, e, q, created.SessionID, action); err != nil {
					t.Fatal(err)
				}
				want := "local edit"
				if wantSync {
					want = "new"
				}
				if string(getFile(t, filepath.Join(root, "managed.txt"))) != want {
					t.Fatal("wrong sync boundary", action, running)
				}
				if wantSync {
					if _, err := os.Stat(filepath.Join(root, "obsolete.txt")); !os.IsNotExist(err) {
						t.Fatal("obsolete managed file retained", err)
					}
					if !strings.Contains(string(getFile(t, filepath.Join(root, "settings.json"))), `"new"`) {
						t.Fatal("owned JSON keys not updated")
					}
				} else if count(d, "start") != starts {
					t.Fatal("running container was restarted")
				}
				if !strings.Contains(string(getFile(t, filepath.Join(root, "settings.json"))), "personal") {
					t.Fatal("Pi-owned setting lost")
				}
				for file, want := range map[string]string{"sessions/history.json": "history", "unmanaged.txt": "keep"} {
					if string(getFile(t, filepath.Join(root, file))) != want {
						t.Fatal("unmanaged state changed", file)
					}
				}
				after := sessionRecord(t, e, created.SessionID)
				if action != "recreate" && (!reflect.DeepEqual(before.Applied.Inputs.Image, after.Applied.Inputs.Image) || !reflect.DeepEqual(before.Applied.Inputs.Container, after.Applied.Inputs.Container)) {
					t.Fatal("access adopted creation drift")
				}
				if (after.Applied.Fingerprints.Runtime != before.Applied.Fingerprints.Runtime) != wantSync {
					t.Fatal("runtime baseline did not follow sync")
				}
			})
		}
	}
}

func TestStartupSyncFailurePreventsStartAndBaselineCommit(t *testing.T) {
	for _, action := range []string{"open", "start", "shell", "exec"} {
		for _, invalidLive := range []bool{false, true} {
			t.Run(action+map[bool]string{false: "/invalid-profile", true: "/invalid-shared-JSON"}[invalidLive], func(t *testing.T) {
				e, d, q := fixture(t)
				ctx := context.Background()
				created, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				before := sessionRecord(t, e, created.SessionID)
				if invalidLive {
					write(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, created.SessionID).Directory, "harnesses/pi/stores/home/settings.json"), `[]`)
				} else {
					write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
				}
				starts := count(d, "start")
				if err := accessAction(ctx, e, q, created.SessionID, action); err == nil {
					t.Fatal("invalid input ignored")
				}
				if count(d, "start") != starts || !reflect.DeepEqual(before.Applied.Inputs, sessionRecord(t, e, created.SessionID).Applied.Inputs) {
					t.Fatal("failed startup changed applied state")
				}
			})
		}
	}
}

func TestStartupDoesNotNeedProfileContentChangeToRestoreManagedFiles(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/pi/managed.txt"), "authoritative")
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, created.SessionID).Directory, "harnesses/pi/stores/home/managed.txt")
	write(t, live, "temporary edit")
	if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	if string(getFile(t, live)) != "authoritative" {
		t.Fatal("unchanged profile did not restore live copy")
	}
}

func TestInspectionStopAndDeletionDoNotSynchronize(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/pi/managed.txt"), "source")
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, created.SessionID).Directory, "harnesses/pi/stores/home/managed.txt")
	write(t, live, "local")
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	checks := []func() error{
		func() error { _, err := e.List(ctx, ""); return err },
		func() error { _, err := e.Status(ctx, created.SessionID, ""); return err },
		func() error { return e.Logs(ctx, created.SessionID, "", false, "10") },
		func() error { return e.ChangeNetwork(ctx, created.SessionID, "", "secondary", true) },
		func() error { return e.Stop(ctx, created.SessionID, "", false) },
		func() error {
			_, err := e.Delete(ctx, DeleteOptions{Scope: DeleteContainer, Selection: Selection{Targets: []string{created.SessionID}}})
			return err
		},
	}
	for i, check := range checks {
		if err := check(); err != nil {
			t.Fatal(i, err)
		}
		if string(getFile(t, live)) != "local" {
			t.Fatal("non-startup action synchronized files", i)
		}
	}
}
