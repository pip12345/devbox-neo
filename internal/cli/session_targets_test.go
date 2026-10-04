package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"github.com/spf13/cobra"
)

func TestSessionDirectoryTargetsAndBlockerDetails(t *testing.T) {
	e, q, target := namedCLIFixture(t)
	r := sessionRecord(t, e, target)
	ctx := context.Background()
	lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("shell")
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	root := newRoot(e.Docker)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--home", e.Store.Home, "status", target})
	if code := Execute(ctx, root); code != 0 {
		t.Fatal(code, stderr.String())
	}
	text := out.String()
	for _, want := range []string{"Session: " + target, "Active commands: 1", "shell", "host PID", lease.Created.Format("2006-01-02T15:04:05Z07:00")} {
		if !strings.Contains(text, want) {
			t.Fatal("missing actionable attachment detail", want, text)
		}
	}
	if strings.Contains(text, r.ID) {
		t.Fatal("human status exposed the internal identity", text)
	}
	for _, args := range [][]string{{"logs", target}, {"status", q.Workspace, "--name", q.LocalName}, {"edit", target, "--show"}} {
		cmd := newRoot(e.Docker)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SetArgs(append([]string{"--home", e.Store.Home}, args...))
		if err := cmd.ExecuteContext(ctx); err != nil {
			t.Fatal("directory/folder target did not resolve", args, err)
		}
	}
	if _, err := e.Locate(ctx, r.Applied.Creation.Name, ""); err == nil {
		t.Fatal("Docker name was accepted as a saved session directory")
	}
}

func TestInternalIDsAreNotPublicTargets(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"open", id}, {"start", id}, {"stop", id}, {"shell", id},
		{"exec", id, "--", "true"}, {"status", id}, {"logs", id},
		{"recreate", id}, {"rename", id, "--to", "work"}, {"edit", id, "--show"},
		{"copy", id, "--abort"}, {"delete", id, "--container"},
		{"network", "inspect", id}, {"network", "connect", "secondary", id},
		{"ssh", id, "host"},
	} {
		root := New()
		cmd, _, err := root.Find(args)
		if err != nil {
			t.Fatal(args, err)
		}
		if err := cmd.ParseFlags(args[len(strings.Fields(cmd.CommandPath()))-1:]); err != nil {
			t.Fatal(args, err)
		}
		if err := cmd.Args(cmd, cmd.Flags().Args()); err == nil || !strings.Contains(err.Error(), "session directory name") {
			t.Fatal("internal ID remained a public target", args, err)
		}
	}
	for _, args := range [][]string{{"open", ".", "--", id}, {"exec", ".", "--", id}, {"network", "connect", id, "."}} {
		root := New()
		cmd, _, _ := root.Find(args)
		if err := cmd.ParseFlags(args[len(strings.Fields(cmd.CommandPath()))-1:]); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Args(cmd, cmd.Flags().Args()); err != nil {
			t.Fatal("non-target argument was treated as a session ID", args, err)
		}
	}
}

func TestRecreateForceFlagReplacesWithActiveCommands(t *testing.T) {
	e, _, target := namedCLIFixture(t)
	r := sessionRecord(t, e, target)
	ctx := context.Background()
	lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Lease("open"); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	name := ""
	cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	e.OnDiagnostic = diagnosticRenderer(&out)
	cmd.SetArgs([]string{target, "--force"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err, out.String())
	}
	current := sessionRecord(t, e, target)
	if current.ID != r.ID || current.Applied.SetupContainer == r.Applied.SetupContainer {
		t.Fatal("--force did not perform replacement while preserving identity")
	}
	if !strings.Contains(out.String(), "Attached commands will be interrupted") || !strings.Contains(out.String(), "Container-local changes will be lost") {
		t.Fatal("forced replacement did not disclose its effects", out.String())
	}
	if _, exists := e.Docker.Runner.(*dockertest.Daemon).Snapshot(r.Applied.Creation.Name); exists {
		t.Fatal("old runtime survived forced replacement")
	}
}
