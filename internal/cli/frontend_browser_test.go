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
	if err != nil || selected == nil || selected.ID != sessionRecord(t, f.e, name).ID {
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
func TestSessionFolderListLabels(t *testing.T) {
	for _, tt := range []struct {
		name        string
		knownFolder string
		want        map[string]string
	}{
		{name: "empty", want: map[string]string{}},
		{name: "single", want: map[string]string{
			"/home/pip/Documents/projects/devbox/rewrite": "rewrite",
		}},
		{name: "distinct", want: map[string]string{
			"/work/api": "api", "/work/web": "web",
		}},
		{name: "duplicate basenames", want: map[string]string{
			"/work/devbox/rewrite": "devbox/rewrite", "/work/other/rewrite": "other/rewrite",
		}},
		{name: "deep shared suffix", want: map[string]string{
			"/home/pip/devbox/rewrite": "pip/devbox/rewrite", "/home/sam/devbox/rewrite": "sam/devbox/rewrite",
		}},
		{name: "complete suffix and root", want: map[string]string{
			"/": "/", "/app": "/app", "/work/app": "work/app",
		}},
		{name: "whole components and unicode", want: map[string]string{
			"/work/app": "app", "/work/myapp": "myapp", "/home/项目": "项目",
		}},
		{name: "empty known folder participates", knownFolder: "/other/rewrite", want: map[string]string{
			"/work/rewrite": "work/rewrite", "/other/rewrite": "other/rewrite",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := frontend{knownFolder: tt.knownFolder}
			var report app.InventoryReport
			for folder := range tt.want {
				if folder != tt.knownFolder {
					report.Sessions = append(report.Sessions, app.View{Workspace: folder, Target: folder + ":main", LocalName: "main"})
				}
			}
			collection := f.sessionCollection(report, "name")
			got := make(map[string]string)
			previous := ""
			for _, item := range collection.Items {
				if !item.Folder {
					if item.Label != "main" || item.ListLabel != "" || item.Depth != 1 {
						t.Fatalf("session row changed: %+v", item)
					}
					continue
				}
				if item.Label != item.Key || item.Open == nil || item.Key < previous {
					t.Fatalf("folder identity, details, action or ordering changed: %+v", item)
				}
				previous = item.Key
				got[item.Key] = item.ListLabel
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("labels = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstSessionInFolderUsesDisplayedOrder(t *testing.T) {
	f := frontend{}
	c := f.sessionCollection(app.InventoryReport{Sessions: []app.View{
		{Workspace: "/work/z", Target: "z-last", LocalName: "zulu"},
		{Workspace: "/work/a", Target: "a-first", LocalName: "alpha"},
		{Workspace: "/work/z", Target: "z-first", LocalName: "alpha"},
	}}, "name")
	for _, tt := range []struct{ folder, want string }{
		{"/work/z", "z-first"}, {"/work/a", "a-first"}, {"/work/missing", ""},
	} {
		if got := firstSessionInFolder(c, tt.folder); got != tt.want {
			t.Fatalf("first session in %s = %q, want %q", tt.folder, got, tt.want)
		}
	}
	f.knownFolder = "/work/empty"
	if got := firstSessionInFolder(f.sessionCollection(app.InventoryReport{}, "name"), "/work/empty"); got != "" {
		t.Fatal("empty folder selected a session", got)
	}
}

func TestCreationCallbackRetriesSameDraft(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("8\n8\n"))
	p, err := newSourcePicker(f.m, f.s.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	original := sessionCreationDraft{workspace: q.Workspace, name: "Second", sources: append([]config.Reference(nil), q.Sources...), makeDefault: true}
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
	f, out, q, _ := frontendFixture(t, strings.NewReader("1\nSecond\n2\n1\n7\n8\n0\n"))
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
	if _, err := f.e.Locate(context.Background(), q.Workspace, "Second"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed build published session", err)
	}
	selected, err := f.e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal("creation selected a default", selected, err)
	}
}
func TestFrontendCreationReturnsToStoppedSessionMenu(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("1\nSecond\n2\n1\n8\n0\n"))
	if err := f.createSessionIn(q.Workspace); err != nil {
		t.Fatal(err, out.String())
	}
	created, err := f.e.Locate(context.Background(), q.Workspace, "Second")
	if err != nil {
		t.Fatal(err)
	}
	name := created.Directory
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
	if !f.defaultAction(app.View{Target: "unmatched"}).Hidden {
		t.Fatal("offered default selection for container without session identity")
	}
}
