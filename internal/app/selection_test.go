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
	a := sessionRecord(t, e, first.SessionID)
	if a.Settings.LocalName != "test" || !environment.IsSessionID(first.SessionID) {
		t.Fatal(a.Settings.Binding)
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
	b := sessionRecord(t, e, second.SessionID)
	if a.ID == b.ID || a.Directory == b.Directory {
		t.Fatal("shared session identity")
	}
	if err := e.SetDefault(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Locate(ctx, q.Workspace, ""); err != nil || got.ID != b.ID {
		t.Fatal("did not select explicit default", got.ID, err)
	}
	if _, err = e.Open(ctx, Request{Workspace: first.SessionID}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Locate(ctx, q.Workspace, ""); err != nil || got.ID != b.ID {
		t.Fatal("exact open changed default", got.ID, err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"ports":["9090:90"]}`)
	pinned := Request{Workspace: a.Settings.Workspace, SessionID: a.ID, Sources: a.Settings.Sources}
	if _, err = e.Recreate(ctx, pinned, false); err != nil {
		t.Fatal(err)
	}
	got := sessionRecord(t, e, first.SessionID)
	if got.ID != a.ID || got.Settings.Binding != a.Settings.Binding || len(got.Applied.Creation.Ports) != 1 || got.Applied.Creation.Ports[0] != "9090:90" {
		t.Fatal(got)
	}
	list, err := e.List(ctx, q.Workspace)
	if err != nil || len(list.Sessions) != 2 {
		t.Fatal(list, err)
	}
	for _, view := range list.Sessions {
		if view.Default != (view.Target == b.Directory) {
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
	if err := e.SetDefault(ctx, sessionRecord(t, e, made.SessionID)); err != nil {
		t.Fatal(err)
	}
	unrelated := environment.ResourceName("/banana", "other", "broken")
	write(t, filepath.Join(e.Store.Home, "sessions", unrelated, "session.json"), "broken")
	r, err := e.Locate(ctx, q.Workspace, "")
	if err != nil || r.ID != made.SessionID {
		t.Fatal(r, err)
	}
	if err = e.Stop(ctx, made.SessionID, "wrong", false); err == nil {
		t.Fatal("ignored conflicting exact selector")
	}
	if _, err = e.Open(ctx, Request{Workspace: made.SessionID, LocalName: "wrong"}); err == nil {
		t.Fatal("ignored conflicting open selector")
	}
	file, _ := e.Store.RecordPath(sessionRecord(t, e, made.SessionID).Directory)
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
			if err := e.SetDefault(ctx, sessionRecord(t, e, made.SessionID)); err != nil {
				t.Fatal(err)
			}
			broken := environment.ResourceName(q.Workspace, name, "broken")
			if name == q.LocalName {
				broken = sessionRecord(t, e, made.SessionID).Directory
			}
			write(t, filepath.Join(e.Store.Home, "sessions", broken, "session.json"), "broken")
			for _, localName := range []string{"", q.LocalName} {
				r, err := e.Locate(ctx, q.Workspace, localName)
				if name == q.LocalName {
					if err == nil {
						t.Fatal("unreadable selected session was ignored")
					}
				} else if err != nil || r.ID != made.SessionID {
					t.Fatal("unrelated session blocked lookup", r.Settings.Binding, err)
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
	r := sessionRecord(t, e, made.SessionID)
	if strings.Contains(strings.Join(r.Applied.Launch.Args, " "), "once") || strings.Contains(strings.Join(r.Applied.Launch.Args, " "), "only-this") || len(r.Applied.Inputs.Runtime.Args) != 0 {
		t.Fatal("persisted invocation arguments", r.Applied.Launch)
	}
}
