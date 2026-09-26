package cli

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
)

func TestSessionDefaultActionSetsAndClearsWithoutPicker(t *testing.T) {
	f, _, q, name := frontendFixture(t, strings.NewReader(""))
	report, err := f.e.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	v := report.Sessions[0]
	a := f.defaultAction(v)
	if a.Label != "Make folder default" || a.Hidden {
		t.Fatal(a)
	}
	if done, err := a.Run(); err != nil || done {
		t.Fatal(done, err)
	}
	selected, err := f.e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected == nil || selected.Name != name {
		t.Fatal(selected, err)
	}
	report, err = f.e.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	v = report.Sessions[0]
	a = f.defaultAction(v)
	if a.Label != "Clear folder default" {
		t.Fatal(a.Label)
	}
	if _, err := a.Run(); err != nil {
		t.Fatal(err)
	}
	selected, err = f.e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal(selected, err)
	}
}
func TestSessionDefaultActionRejectsReplacedIdentity(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader(""))
	report, err := f.e.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	v := report.Sessions[0]
	v.SessionID = "stale"
	if _, err := f.defaultAction(v).Run(); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Finish(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "identity changed") {
		t.Fatal(out.String())
	}
	selected, err := f.e.Store.ReadDefault(context.Background(), v.Workspace)
	if err != nil || selected != nil {
		t.Fatal(selected, err)
	}
}
func TestSessionCollectionContainsOnlyFolderAndSessionObjects(t *testing.T) {
	f, _, q, name := frontendFixture(t, strings.NewReader(""))
	report, err := f.e.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	c := f.sessionCollection(report, "folder")
	if len(c.Items) != 2 || c.Items[0].Key != q.Workspace || c.Items[1].Key != name || c.Items[1].Depth != 1 {
		t.Fatal(c)
	}
	if c.Items[0].Open == nil || c.Items[1].Open == nil {
		t.Fatal("objects cannot be opened")
	}
	for _, field := range c.Items[1].Fields {
		if field.Label == "Error" || field.Label == "Home" || field.Label == "Version" {
			t.Fatal("unrelated or empty field", field)
		}
	}
}
func TestCreationCallbackRetriesSameDraft(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("7\n7\n"))
	p, err := newSourcePicker(f.m, f.s.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	original := sessionCreationDraft{workspace: q.Workspace, name: "Second", sources: append([]config.Reference(nil), q.Sources...)}
	attempts := 0
	after, created, err := sessionCreationMenu(p, f.e, original, func(draft sessionCreationDraft) (bool, error) {
		attempts++
		if !reflect.DeepEqual(original, draft) {
			t.Fatal("retry lost draft", draft)
		}
		if attempts == 1 {
			f.m.Notice("Build failed")
			return false, nil
		}
		return true, nil
	})
	if err != nil || !created || attempts != 2 || !reflect.DeepEqual(after, original) {
		t.Fatal(after, created, attempts, err, out.String())
	}
}
func TestFrontendBuildFailurePreservesDraftAndDoesNotCreateDefault(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("1\nSecond\n2\n1\n7\n0\n"))
	daemon := f.e.Docker.Runner.(*dockertest.Daemon)
	daemon.Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "build" {
			return errors.New("injected build failure")
		}
		return nil
	}
	if err := f.createSessionIn(q.Workspace); err != nil {
		t.Fatal(err, out.String())
	}
	if strings.Count(out.String(), "Session name: Second") < 2 || !strings.Contains(out.String(), "injected build failure") {
		t.Fatal("failed build discarded draft", out.String())
	}
	if _, err := f.e.Store.Read(context.Background(), environment.ContainerName(q.Workspace, "Second")); !os.IsNotExist(err) {
		t.Fatal("failed build published session", err)
	}
	selected, err := f.e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal("creation selected a default", selected, err)
	}
}
func TestFrontendCreationReturnsToStoppedSessionMenu(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("1\nSecond\n2\n1\n7\n0\n"))
	if err := f.createSessionIn(q.Workspace); err != nil {
		t.Fatal(err, out.String())
	}
	name := environment.ContainerName(q.Workspace, "Second")
	details, err := f.e.Status(context.Background(), name, "")
	if err != nil || details.Running || len(details.Active) != 0 {
		t.Fatal("creation launched or attached", details, err)
	}
	selected, err := f.e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal(selected, err)
	}
	if !strings.Contains(out.String(), "Session · Second") || !strings.Contains(out.String(), "Make folder default") {
		t.Fatal("creation did not land on session menu", out.String())
	}
	if f.focusItem != name {
		t.Fatal("new session not selected for browser return", f.focusItem)
	}
}
func TestUnmatchedContainerHasNoDefaultAction(t *testing.T) {
	f, _, _, _ := frontendFixture(t, strings.NewReader(""))
	if !f.defaultAction(app.View{Name: "unmatched"}).Hidden {
		t.Fatal("offered default selection for container without session identity")
	}
}
