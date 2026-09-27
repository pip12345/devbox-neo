package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestListDetailsAndSorting(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	views := []app.View{
		{Target: "b", Exists: true, SessionID: "api", LocalName: "basic", Harness: "pi", Workspace: "/work/api", LastActivity: now.Add(-2 * time.Hour), LastAction: "open", CreatedAt: now.Add(-24 * time.Hour)},
		{Target: "a", Exists: true, Running: true, SessionID: "project", Harness: "opencode", Workspace: "/work/ui", LastActivity: now},
		{Target: "unknown", Exists: true, Error: "no durable record", Pending: &store.Reservation{Mode: "clone", Phase: "prepare", Source: "a", Destination: "b"}},
	}
	sortViews(views, "last-active")
	if views[0].Target != "a" || views[1].Target != "b" || views[2].Target != "unknown" {
		t.Fatal("activity sort must put newest first and unknown last", views)
	}
	var out bytes.Buffer
	if err := printSessionList(&out, views, false, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FOLDER", "NAME", "DEFAULT", "CONFIGS", "LIFETIME", "LAST ACTIVE", "/work/api", "/work/ui", "2 hours ago", "just now", "stopped!*", "no durable record", "pending copy"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if !strings.Contains(out.String(), "HARNESS") || !strings.Contains(out.String(), "opencode") {
		t.Fatal("default list lost harness details", out.String())
	}
	out.Reset()
	if err := printSessionList(&out, views, true, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HARNESS", "opencode", "LAST ACTION", "CREATED", "SESSION ID", "2026-09-01T10:00:00Z", "2026-08-31T12:00:00Z", "open"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing wide detail %q: %s", want, out.String())
		}
	}
	views[0].LastActivity = views[1].LastActivity
	sortViews(views, "last-active")
	if views[0].Target != "a" {
		t.Fatal("equal activity must use name as tie breaker")
	}
	sortViews(views, "name")
	if views[0].Target != "a" {
		t.Fatal("name ordering changed")
	}
	sortViews(views, "folder")
	if views[0].Target != "unknown" || views[1].Target != "b" || views[2].Target != "a" {
		t.Fatal("folder sort must put unknown paths first and sort names within each folder", views)
	}
}

func TestListLifetimeFollowsExplicitStartAndStop(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	show := func() string {
		t.Helper()
		report, err := e.List(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := printSessionList(&out, report.Sessions, false, time.Now()); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if text := show(); !strings.Contains(text, "automatic") {
		t.Fatal("new session should stop automatically", text)
	}
	if _, err := e.Start(context.Background(), name, ""); err != nil {
		t.Fatal(err)
	}
	if text := show(); !strings.Contains(text, "running") || !strings.Contains(text, "until stop") {
		t.Fatal("explicit start did not appear as keep-running intent", text)
	}
	if err := e.Stop(context.Background(), name, "", false); err != nil {
		t.Fatal(err)
	}
	if text := show(); !strings.Contains(text, "stopped") || !strings.Contains(text, "automatic") {
		t.Fatal("stop did not clear keep-running intent", text)
	}
}

func TestSessionListShowsDurableStateAndDiagnostics(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	views := []app.View{
		{Target: "recent", SessionID: "project", Harness: "pi", Workspace: "/work/project", LastActivity: now},
		{Target: "older", SessionID: "older-id", ManualStart: true, LocalName: "basic", Harness: "opencode", Exists: true, Workspace: "/work/project", LastActivity: now.Add(-2 * time.Hour)},
		{Target: "separate", Workspace: "/work/api"},
		{Target: "broken", Error: "corrupt record", Pending: &store.Reservation{Mode: "relocate", Phase: "prepare", Source: "older", Destination: "broken"}},
	}
	sortViews(views, "last-active")
	var out bytes.Buffer
	if err := printSessionList(&out, views, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "FOLDER NAME DEFAULT HARNESS LAST ACTIVE CONTAINER LIFETIME CONFIGS") {
		t.Fatal("not a session-focused table", out.String())
	}
	for _, want := range []string{"pi", "/work/project", "just now", "missing", "opencode", "older", "2 hours ago", "stopped", "missing!*", "corrupt record", "pending copy --move", "automatic", "until stop"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	names := []string{"recent", "basic", "broken", "separate"}
	for i := 1; i < len(names); i++ {
		if strings.Index(out.String(), names[i-1]) >= strings.Index(out.String(), names[i]) {
			t.Fatal("last-active sort did not apply across folders", out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("non-terminal output contains styling")
	}
	if !strings.HasPrefix(out.String(), "FOLDER") || strings.Contains(out.String(), "\n\n") {
		t.Fatal("global list retained folder headings or blank lines", out.String())
	}
}

func TestListUsesLocalNamesAndWideIncludesFullNames(t *testing.T) {
	views := []app.View{
		{Target: "devbox-alpha-111111111111.work", SessionID: "session-alpha", ContainerName: "container-alpha", LocalName: "work", Workspace: "/projects/alpha"},
		{Target: "devbox-beta-222222222222.work", SessionID: "session-beta", ContainerName: "container-beta", LocalName: "work", Workspace: "/projects/beta"},
	}
	for _, local := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			rows := views
			nameColumn := 1
			if local {
				rows = views[:1]
				nameColumn = 0
			}
			var out bytes.Buffer
			if err := printSessionTable(&out, rows, wide, time.Now(), local); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if strings.Contains(lines[0], "SESSION ID") != wide || strings.Contains(lines[0], "FOLDER") == local {
				t.Fatal("wrong columns", out.String())
			}
			for i, view := range rows {
				fields := strings.Fields(lines[i+1])
				if fields[nameColumn] != view.LocalName || strings.Contains(lines[i+1], view.SessionID) != wide {
					t.Fatal("full name replaced the local name or leaked into the compact row", out.String())
				}
				if !local && fields[0] != view.Workspace {
					t.Fatal("duplicate local names lost their folder context", out.String())
				}
			}
		}
	}
}

func TestListNamePresentationDoesNotChangeJSONOrStatus(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	factory := func(*cobra.Command) (*app.Engine, error) { return e, nil }
	for _, folder := range []string{"", q.Workspace} {
		for _, wide := range []bool{false, true} {
			localName := ""
			cmd := sessionCommands(factory, &localName)[0]
			args := []string{}
			if folder != "" {
				args = append(args, folder)
			}
			if wide {
				args = append(args, "--wide")
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(args)
			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), fullName) != wide || !strings.Contains(out.String(), q.LocalName) {
				t.Fatal("list command did not apply name presentation", out.String())
			}
		}
	}
	localName := ""
	cmd := sessionCommands(factory, &localName)[0]
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report app.InventoryReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].SessionID != fullName || report.Sessions[0].LocalName != q.LocalName {
		t.Fatal("list JSON lost exact identity", out.String())
	}
	out.Reset()
	cmd = statusCommand(factory, &localName)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{q.Workspace, "--name", q.LocalName})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), fullName+"  ") {
		t.Fatal("status must retain the exact container name", out.String())
	}
}

func TestListTimesAndUnsafeCells(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		at   time.Time
		want string
	}{{time.Time{}, "-"}, {now.Add(time.Hour), "just now"}, {now.Add(-time.Minute), "1 minute ago"}, {now.Add(-25 * time.Hour), "1 day ago"}} {
		if got := activityAge(tt.at, now); got != tt.want {
			t.Fatalf("got %s, want %s", got, tt.want)
		}
	}
	views := []app.View{{Target: "full\x1b[31m", LocalName: "local\nforged", Harness: "pi\nforged", Workspace: "/work/\nforged\t\x1b[31m"}}
	var out bytes.Buffer
	if err := printSessionList(&out, views, true, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || strings.Count(out.String(), "\n") != 2 {
		t.Fatal("unsafe path changed table structure", out.String())
	}
	out.Reset()
	if err := printSessionList(&out, views, false, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || strings.Count(out.String(), "\n") != 2 {
		t.Fatal("unsafe session fields changed table structure", out.String())
	}
}
