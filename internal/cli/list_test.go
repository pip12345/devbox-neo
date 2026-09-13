package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/store"
)

func TestListDetailsAndSorting(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	views := []app.View{
		{Name: "b", Exists: true, Profile: "basic", Harness: "pi", Workspace: "/work/api", LastActivity: now.Add(-2 * time.Hour), LastAction: "open", CreatedAt: now.Add(-24 * time.Hour)},
		{Name: "a", Exists: true, Running: true, SessionID: "project", Harness: "opencode", Workspace: "/work/ui", LastActivity: now},
		{Name: "unknown", Exists: true, Error: "no durable record", Pending: &store.Reservation{Mode: "clone", Phase: "prepare", Source: "a", Destination: "b"}},
	}
	sortViews(views, "last-active")
	if views[0].Name != "a" || views[1].Name != "b" || views[2].Name != "unknown" {
		t.Fatal("activity sort must put newest first and unknown last", views)
	}
	var out bytes.Buffer
	if err := printSessionList(&out, views, false, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "PROFILE", "LAST ACTIVE", ".project", "2 hours ago", "just now", "stopped!*", "no durable record", "pending clone"} {
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
	for _, want := range []string{"HARNESS", "opencode", "LAST ACTION", "CREATED", "2026-09-01T10:00:00Z", "2026-08-31T12:00:00Z", "open"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing wide detail %q: %s", want, out.String())
		}
	}
	views[0].LastActivity = views[1].LastActivity
	sortViews(views, "last-active")
	if views[0].Name != "a" {
		t.Fatal("equal activity must use name as tie breaker")
	}
	sortViews(views, "name")
	if views[0].Name != "a" {
		t.Fatal("name ordering changed")
	}
}

func TestSessionListShowsDurableStateAndDiagnostics(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	views := []app.View{
		{Name: "recent", SessionID: "project", Harness: "pi", Workspace: "/work/project", LastActivity: now},
		{Name: "older", Profile: "basic", Harness: "opencode", Exists: true, Workspace: "/work/api", LastActivity: now.Add(-2 * time.Hour)},
		{Name: "broken", Error: "corrupt record", Pending: &store.Reservation{Mode: "clone", Phase: "prepare", Source: "older", Destination: "broken"}},
	}
	sortViews(views, "last-active")
	var out bytes.Buffer
	if err := printSessionList(&out, views, false, now); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "NAME HARNESS PROFILE LAST ACTIVE CONTAINER FOLDER" {
		t.Fatal("not a session-focused table", lines[0])
	}
	for _, want := range []string{"pi", ".project", "just now", "missing", "opencode", "basic", "2 hours ago", "stopped", "missing!*", "corrupt record", "pending clone"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if !strings.HasPrefix(lines[1], "recent ") || !strings.HasPrefix(lines[2], "older ") || !strings.HasPrefix(lines[3], "broken ") {
		t.Fatal("incorrect session order", out.String())
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("non-terminal output contains styling")
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
	views := []app.View{{Name: "test", Harness: "pi\nforged", Workspace: "/work/\nforged\t\x1b[31m"}}
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
