package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
)

func TestOpenRequiresExplicitCreation(t *testing.T) {
	for _, project := range []bool{false, true} {
		t.Run(map[bool]string{false: "profile", true: "project"}[project], func(t *testing.T) {
			e, d, q := fixture(t)
			if project {
				q.Profile = ""
				write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi","inherit_profile":false}`)
			}
			ctx := context.Background()
			_, err := e.Open(ctx, q)
			var missing *commanderror.Error
			if !errors.Is(err, os.ErrNotExist) || !errors.As(err, &missing) || missing.Code != "session_missing" {
				t.Fatal(err)
			}
			want := []commanderror.Step{
				commanderror.Next("Create", "create", q.Workspace),
			}
			if !reflect.DeepEqual(missing.Next, want) {
				t.Fatal(missing.Next)
			}
			entries, err := os.ReadDir(filepath.Join(e.Store.Home, "sessions"))
			if err != nil || len(entries) != 0 || len(d.History()) != 0 {
				t.Fatal("plain open created state or touched Docker", entries, err, d.History())
			}
			opened, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			first := record(t, e, opened.Name)
			for range 2 {
				if _, err = e.Open(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			if count(d, "create") != 1 || count(d, "build") != 1 || record(t, e, opened.Name).ID != first.ID {
				t.Fatal("opening an existing session replaced it")
			}
		})
	}
}

func TestCreatePreparesWithoutOpeningAndLeavesStopped(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","on_exit":"running"}`)
	write(t, filepath.Join(e.Store.Home, "profiles/test/setup.sh"), "echo setup\n")
	write(t, filepath.Join(e.Store.Home, "profiles/test/entrypoint.sh"), "echo entrypoint\n")
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, result.Name)
	c, exists := d.Snapshot(result.Name)
	if !exists || c.State.Running || r.Action != "create" || r.SetupContainer != c.ID {
		t.Fatal("create did not commit a prepared stopped environment", r, c)
	}
	launch := append([]string{r.Launch.Binary}, r.Launch.Args...)
	hooks := 0
	for _, args := range d.History() {
		if args[0] == "exec" && argvSuffix(args, launch) {
			t.Fatal("create launched the harness")
		}
		if args[0] == "exec" && argvSuffix(args, []string{"bash", "-s"}) {
			hooks++
		}
	}
	if hooks != 1 {
		t.Fatal("create must run setup, not entrypoint", hooks)
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	c, _ = d.Snapshot(result.Name)
	if !c.State.Running || count(d, "create") != 1 || record(t, e, result.Name).ID != r.ID {
		t.Fatal("open did not reuse the created environment")
	}
}

func TestOpenLaunchOverridesDoNotRecreate(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := record(t, e, created.Name)
	onExit := "running"
	q.Overrides.OnExit = &onExit
	q.Overrides.HarnessArgs = []string{"--version"}
	q.Continue = true
	q.Args = []string{"--one-off"}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	after := record(t, e, created.Name)
	c, _ := d.Snapshot(created.Name)
	if count(d, "create") != 1 || count(d, "build") != 1 || !c.State.Running || after.Launch.OnExit != "running" || !reflect.DeepEqual(after.Inputs.Container, before.Inputs.Container) || !reflect.DeepEqual(after.Inputs.Image, before.Inputs.Image) {
		t.Fatal("launch overrides changed creation settings or lost on-exit policy")
	}
	want := []string{"pi", "--tui-mode", "fullscreen", "--version", "-c", "--one-off"}
	for _, args := range d.History() {
		if args[0] == "exec" && argvSuffix(args, want) {
			return
		}
	}
	t.Fatal("launch overrides did not reach the harness", d.History())
}

func TestCreateRefusesExistingSessionEvenWithoutContainer(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	first := record(t, e, result.Name)
	for _, missingContainer := range []bool{false, true} {
		if missingContainer {
			d.Forget(result.Name)
		}
		before := len(d.History())
		_, err = e.Create(ctx, q)
		var exists *commanderror.Error
		if !errors.As(err, &exists) || exists.Code != "session_exists" {
			t.Fatal(err)
		}
		if len(d.History()) != before || !reflect.DeepEqual(record(t, e, result.Name), first) {
			t.Fatal("create changed an existing session")
		}
	}
}

func TestExistingSessionRecoveryStillRestoresMissingContainer(t *testing.T) {
	for _, action := range []string{"open", "start"} {
		t.Run(action, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			result, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			first := record(t, e, result.Name)
			d.Forget(result.Name)
			if action == "open" {
				_, err = e.Open(ctx, q)
			} else {
				_, err = e.Start(ctx, result.Name, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if count(d, "create") != 2 || count(d, "build") != 1 || record(t, e, result.Name).ID != first.ID {
				t.Fatal("recorded recovery changed identity or rebuilt the image")
			}
		})
	}
}

func TestConcurrentCreatePublishesOnlyOneSession(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = e.Create(ctx, q)
		}(i)
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		var exists *commanderror.Error
		if !errors.As(err, &exists) || exists.Code != "session_exists" {
			t.Fatal(err)
		}
	}
	if successes != 1 || count(d, "create") != 1 || count(d, "build") != 1 {
		t.Fatal("concurrent create was not serialized", errs)
	}
}

func TestCreateRejectsCorruptOrUncommittedState(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		e, d, q := fixture(t)
		ctx := context.Background()
		spec, err := e.Resolve(q)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(e.Store.Home, "sessions", spec.Identity.Name)
		if corrupt {
			write(t, filepath.Join(dir, "session.json"), "broken")
		} else {
			write(t, filepath.Join(dir, "partial-state"), "retained")
		}
		if _, err = e.Create(ctx, q); err == nil {
			t.Fatal("create accepted existing invalid state")
		}
		if count(d, "create") != 0 || count(d, "build") != 0 {
			t.Fatal("invalid state caused creation")
		}
	}
}

func TestOpenDoesNotInventMissingExactSession(t *testing.T) {
	e, d, q := fixture(t)
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = spec.Identity.Name
	_, err = e.Open(context.Background(), q)
	var missing *commanderror.Error
	if !errors.As(err, &missing) || missing.Code != "session_missing" || len(d.History()) != 0 {
		t.Fatal(err, d.History())
	}
}

func TestCreateStopFailureRetainsPreparedSession(t *testing.T) {
	e, d, q := fixture(t)
	failure := &docker.ExitError{Code: 19, Operation: "stop"}
	d.Fail = func(args []string) error {
		if args[0] == "stop" {
			return failure
		}
		return nil
	}
	result, err := e.Create(context.Background(), q)
	var stop *commanderror.Error
	if !errors.Is(err, failure) || !errors.As(err, &stop) || stop.Code != "create_stop_failed" {
		t.Fatal(err)
	}
	if record(t, e, result.Name).Action != "create" || count(d, "rm") != 0 {
		t.Fatal("stop failure lost the committed session")
	}
}
