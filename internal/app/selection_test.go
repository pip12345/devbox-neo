package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/environment"
)

func TestNamedSessionsRequireExplicitDefaultsAndPinSources(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	a := record(t, e, first.Name)
	if a.Identity.LocalName != "test" || !strings.HasSuffix(first.Name, ".test") {
		t.Fatal(a.Identity)
	}
	// Neither a sole session nor an obsolete global config selects a default.
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"test"}`)
	calls := len(d.History())
	for _, action := range []func() error{
		func() error { _, err := e.Open(ctx, Request{Workspace: q.Workspace}); return err },
		func() error { _, err := e.Start(ctx, q.Workspace, ""); return err },
		func() error { return e.Stop(ctx, q.Workspace, "", false) },
		func() error { return e.Exec(ctx, q.Workspace, "", []string{"true"}, false) },
		func() error { _, err := e.Status(ctx, q.Workspace, ""); return err },
		func() error { return e.Logs(ctx, q.Workspace, "", false, "1") },
	} {
		if err := action(); err == nil {
			t.Fatal("selected an implicit default")
		}
	}
	if len(d.History()) != calls {
		t.Fatal("missing selection touched Docker")
	}
	q.LocalName = "other"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	b := record(t, e, second.Name)
	if a.ID == b.ID || a.Identity.Name == b.Identity.Name {
		t.Fatal("shared session identity")
	}
	if err := e.SetDefault(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Locate(ctx, q.Workspace, ""); err != nil || got.ID != b.ID {
		t.Fatal("did not select explicit default", got.ID, err)
	}
	if _, err = e.Open(ctx, Request{Workspace: first.Name}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Locate(ctx, q.Workspace, ""); err != nil || got.ID != b.ID {
		t.Fatal("exact open changed default", got.ID, err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"ports":["9090:90"]}`)
	pinned := Request{Workspace: a.Identity.Workspace, Recorded: &a.Identity, Sources: a.Sources}
	if _, err = e.Recreate(ctx, pinned, false); err != nil {
		t.Fatal(err)
	}
	got := record(t, e, first.Name)
	if got.ID != a.ID || got.Identity != a.Identity || len(got.Creation.Ports) != 1 || got.Creation.Ports[0] != "9090:90" {
		t.Fatal(got)
	}
	list, err := e.List(ctx, q.Workspace)
	if err != nil || len(list.Sessions) != 2 {
		t.Fatal(list, err)
	}
	for _, view := range list.Sessions {
		if view.Default != (view.Name == b.Identity.Name) {
			t.Fatal("incorrect default marker", view)
		}
	}
}

func TestDirectLookupIgnoresUnrelatedCorruptionAndRejectsConflictingSelectors(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetDefault(ctx, record(t, e, made.Name)); err != nil {
		t.Fatal(err)
	}
	unrelated := environment.ContainerName("/banana", "other")
	write(t, filepath.Join(e.Store.Home, "sessions", unrelated, "session.json"), "broken")
	r, err := e.Locate(ctx, q.Workspace, "")
	if err != nil || r.Identity.Name != made.Name {
		t.Fatal(r, err)
	}
	if err = e.Stop(ctx, made.Name, "wrong", false); err == nil {
		t.Fatal("ignored conflicting exact selector")
	}
	if _, err = e.Open(ctx, Request{Workspace: made.Name, LocalName: "wrong"}); err == nil {
		t.Fatal("ignored conflicting open selector")
	}
	file, _ := e.Store.RecordPath(made.Name)
	if err = os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Locate(ctx, q.Workspace, ""); err == nil {
		t.Fatal("ignored corrupt target")
	}
}

func TestFolderLookupOnlyReadsSelectedSession(t *testing.T) {
	for _, name := range []string{"other", "test", "Test"} {
		t.Run(name, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetDefault(ctx, record(t, e, made.Name)); err != nil {
				t.Fatal(err)
			}
			broken := environment.ContainerName(q.Workspace, name)
			write(t, filepath.Join(e.Store.Home, "sessions", broken, "session.json"), "broken")
			for _, localName := range []string{"", q.LocalName} {
				r, err := e.Locate(ctx, q.Workspace, localName)
				if name == q.LocalName {
					if err == nil {
						t.Fatal("unreadable selected session was ignored")
					}
				} else if err != nil || r.Identity.Name != made.Name {
					t.Fatal("unrelated session blocked lookup", r.Identity, err)
				}
			}
		})
	}
}

func TestInvocationHarnessArgumentsAreNotSaved(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.HarnessArgs = []string{"--only-this-time"}
	q.Args = []string{"--also-once"}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	r := record(t, e, made.Name)
	if strings.Contains(strings.Join(r.Launch.Args, " "), "once") || strings.Contains(strings.Join(r.Launch.Args, " "), "only-this") || len(r.Inputs.Runtime.Args) != 0 {
		t.Fatal("persisted invocation arguments", r.Launch)
	}
}
