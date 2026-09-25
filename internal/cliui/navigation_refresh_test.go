package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNestedRefreshSurvivesReturningToParent(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("1\n1\n0\n"), &out)
	c := browserFixture()
	r.navigation = &Navigation{Collection: c, Key: "session-a", Query: "pi"}
	shown := snapshotNavigation(r.navigation)
	builds := 0
	err := r.Run(func() (Screen, error) {
		builds++
		if builds == 2 && (r.navigation.Collection.Items[1].Status != "stopped" || r.navigation.Key != "session-a" || r.navigation.Query != "pi") {
			t.Fatal("parent restored stale navigation", r.navigation)
		}
		return Screen{Title: "Parent", Back: "Back", Actions: []Action{{Label: "Child", Run: func() (bool, error) {
			return false, r.Run(func() (Screen, error) {
				return Screen{Title: "Child", Back: "Back", Actions: []Action{{Label: "Refresh", Run: func() (bool, error) {
					fresh := browserFixture()
					fresh.Items[1].Status = "stopped"
					r.RefreshNavigation(fresh)
					return true, nil
				}}}}, nil
			})
		}}}}, nil
	})
	if err != nil || builds != 2 {
		t.Fatal(builds, err, out.String())
	}
	if shown.Collection.Items[1].Status != "missing" {
		t.Fatal("refresh mutated a presented snapshot")
	}
	r.RefreshNavigation(Collection{Title: "Configs"})
	if r.navigation.Collection.Title != "Sessions" {
		t.Fatal("refresh replaced a different browser")
	}
}

func TestActivityRowsFitAndRetainSelection(t *testing.T) {
	for _, width := range []int{28, 38, 48, 56} {
		req := browserRequest()
		req.page.Collection.Items[1].Activity = "5 minutes ago"
		req.page.Collection.Items[2].Activity = "3 days ago"
		m := newTerminalModel(req, true)
		key(m, tea.KeyDown, "")
		key(m, tea.KeyDown, "")
		view := m.objectList(*req.page.Collection, "", "", width, 12, true)
		plain := ansi.Strip(view)
		for _, want := range []string{"Last active", "5 minutes ago", "3 days ago", "claude *"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d lost %q:\n%s", width, want, plain)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
		key(m, tea.KeyEnter, "")
		if reply := <-req.reply; !reply.item || reply.index != 2 {
			t.Fatal("activity rows changed dispatch", reply)
		}
	}
}
