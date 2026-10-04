package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestContinueUsesAppliedHooksWithoutSourceDirectories(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(q.Sources[0].Path, "before-open.sh"), "echo original")
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, made.SessionID)
	if err := os.RemoveAll(q.Sources[0].Path); err != nil {
		t.Fatal(err)
	}
	q.Continue = true
	before := len(d.History())
	if _, err := e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	path := docker.OpenHookPath(environment.Digest([]byte("echo original")))
	found := false
	for _, args := range d.History()[before:] {
		if argvSuffix(args, []string{"bash", path}) {
			found = true
		}
	}
	if !found || string(d.Hooks[old.Applied.SetupContainer][path]) != "echo original" {
		t.Fatal("applied hook was not executed")
	}
	current := sessionRecord(t, e, made.SessionID)
	if !reflect.DeepEqual(old.Applied, current.Applied) {
		t.Fatal("access changed applied state")
	}
}

func TestOlderContainerRequiresExplicitHookApplicationButAllowsRepair(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	script := filepath.Join(q.Sources[0].Path, "before-open.sh")
	write(t, script, "echo old")
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, made.SessionID)
	delete(d.Hooks, old.Applied.SetupContainer)
	write(t, script, "echo current")
	var failure *commanderror.Error
	if _, err := e.Open(ctx, q); !errors.As(err, &failure) || failure.Code != "applied_hooks_unavailable" {
		t.Fatal("missing installed hooks were silently bypassed", err)
	}
	if err := e.Exec(ctx, made.SessionID, "", []string{"true"}, true); err != nil {
		t.Fatal("missing hooks blocked repair shell", err)
	}
	if _, err := e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	if current := sessionRecord(t, e, made.SessionID); current.Applied.SetupContainer != old.Applied.SetupContainer {
		t.Fatal("hook application replaced container")
	}
	if _, err := e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
}
