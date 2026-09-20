package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/environment"
)

func TestCompoundSessionsRequireSeparateCreationAndPinSources(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"test"}`)
	write(t, filepath.Join(e.Store.Home, "profiles/other/config.json"), `{"harness":"pi"}`)
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"ports":["8080:80"]}`)
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	a := record(t, e, first.Name)
	if !a.Identity.Project || a.Identity.Profile != "test" || !strings.HasSuffix(first.Name, ".profile-test.project") {
		t.Fatal(a.Identity)
	}
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"other"}`)
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
			t.Fatal("substituted another profile")
		}
	}
	if len(d.History()) != calls {
		t.Fatal("missing selection touched Docker")
	}
	q.Profile = "other"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	b := record(t, e, second.Name)
	if a.ID == b.ID || a.Identity.Name == b.Identity.Name || a.Identity.Slot == b.Identity.Slot {
		t.Fatal("shared compound identity")
	}
	if _, err = e.Open(ctx, Request{Workspace: first.Name}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"other","ignore_project":true}`)
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"ports":["9090:90"]}`)
	pinned := Request{Workspace: a.Identity.Workspace, Recorded: &a.Identity, Sources: a.Sources}
	if _, err = e.Recreate(ctx, pinned, false); err != nil {
		t.Fatal(err)
	}
	got := record(t, e, first.Name)
	if got.ID != a.ID || got.Identity != a.Identity || len(got.Creation.Ports) != 1 || got.Creation.Ports[0] != "9090:90" {
		t.Fatal(got)
	}
	list, err := e.List(ctx, "test")
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].Name != first.Name {
		t.Fatal(list, err)
	}
	q.IgnoreProject = true
	q.Profile = "test"
	plain, err := e.Create(ctx, q)
	if err != nil || plain.Name == first.Name || !strings.HasSuffix(plain.Name, ".profile-test") {
		t.Fatal(plain, err)
	}
}

func TestDirectLookupIgnoresUnrelatedCorruptionAndRejectsConflictingSelectors(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"test"}`)
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := environment.ContainerName("/banana", "project")
	write(t, filepath.Join(e.Store.Home, "sessions", unrelated, "session.json"), "broken")
	r, err := e.Locate(ctx, q.Workspace, "")
	if err != nil || r.Identity.Name != made.Name {
		t.Fatal(r, err)
	}
	if err = e.Stop(ctx, made.Name, "wrong", false); err == nil {
		t.Fatal("ignored conflicting exact selector")
	}
	if _, err = e.Open(ctx, Request{Workspace: made.Name, Profile: "wrong"}); err == nil {
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

func TestProjectBindingLookupOnlyReadsMatchingSlots(t *testing.T) {
	for _, tt := range []struct {
		slot string
		fail bool
	}{
		{"profile-other", false},
		{"profile-other.project", false},
		{"profile-test", false},
		{"profile-test.project", true},
		{"project", true},
	} {
		t.Run(tt.slot, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			write(t, filepath.Join(e.Store.Home, "config.json"), `{"default_profile":"test"}`)
			q.ProjectDir = t.TempDir()
			write(t, filepath.Join(q.ProjectDir, "config.json"), `{}`)
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			broken := environment.ContainerName(q.Workspace, tt.slot)
			write(t, filepath.Join(e.Store.Home, "sessions", broken, "session.json"), "broken")
			for _, profile := range []string{"", q.Profile} {
				r, err := e.Locate(ctx, q.Workspace, profile)
				if tt.fail {
					if err == nil || !strings.Contains(err.Error(), "unreadable") {
						t.Fatal("unreadable relevant binding was ignored", err)
					}
				} else if err != nil || r.Identity.Name != made.Name {
					t.Fatal("unrelated slot blocked lookup", r.Identity, err)
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
	q.Overrides.HarnessArgs = []string{"--only-this-time"}
	q.Args = []string{"--also-once"}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	r := record(t, e, made.Name)
	if strings.Contains(strings.Join(r.Launch.Args, " "), "once") || strings.Contains(strings.Join(r.Launch.Args, " "), "only-this") || len(r.Inputs.Runtime.Args) != 0 {
		t.Fatal("persisted invocation arguments", r.Launch)
	}
}
