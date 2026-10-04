package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func browserFixture() Collection {
	return Collection{Title: "Sessions", Empty: "No sessions yet.", Items: []Item{
		{Key: "/project", Label: "/project", Folder: true, Fields: []Field{{Label: "Default", Value: "claude"}}, Actions: []Action{{Label: "Create session here"}}},
		{Key: "session-a", Label: "pi", Depth: 1, Status: "missing", Description: "/project pi", Actions: []Action{{Label: "Continue", Shortcut: "c"}, {Label: "Recreate", Shortcut: "r"}}},
		{Key: "session-b", Label: "claude", Depth: 1, Status: "stopped", Selected: true, Description: "/project claude", Fields: []Field{
			{Label: "Folder", Value: "/project"}, {Label: "Container", Value: "stopped", Status: true}, {Label: "Configs", Value: "1. base\n2. project"},
		}, Actions: []Action{{Label: "Continue", Shortcut: "c"}, {Label: "Recreate", Shortcut: "r"}}},
	}}
}
func browserRequest() *screenRequest {
	req := request(Action{Label: "Create session", Shortcut: "n"}, Action{Label: "All-session operations", Shortcut: "a"}, Action{Label: "Refresh", Shortcut: "r"})
	collection := browserFixture()
	req.page.Collection = &collection
	req.page.Title = "Sessions"
	req.canTab = true
	return req
}
func TestBrowserSeparatesObjectsAndCommands(t *testing.T) {
	req := browserRequest()
	m := newTerminalModel(req, true)
	m.width, m.height = 112, 34
	key(m, tea.KeyDown, "")
	key(m, tea.KeyDown, "")
	view := ansi.Strip(m.View().Content)
	for _, forbidden := range []string{"D B X", "INTERACTIVE WORKSPACE", "no background polling", "CONTEXT / DETAILS", "Default: true", "n create", "a bulk"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("UI contains discarded copy %q", forbidden)
		}
	}
	for _, label := range []string{"Create session", "All-session operations", "Refresh"} {
		if strings.Count(view, label) != 1 {
			t.Fatalf("command repeated: %s\n%s", label, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		before, _, ok := strings.Cut(line, "│")
		if ok && (strings.Contains(before, "Create session") || strings.Contains(before, "Refresh")) {
			t.Fatal("commands leaked into object panel", line)
		}
	}
	if !strings.Contains(view, "claude *") || !strings.Contains(view, "1. base") || !strings.Contains(view, "2. project") {
		t.Fatal(view)
	}
	if strings.Count(view, "Tab") != 1 || !strings.Contains(strings.Split(view, "\n")[0], "Configs") {
		t.Fatal("tabs are not in the header", view)
	}
	if !strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("structured details lost colors")
	}
	key(m, tea.KeyEnter, "")
	reply := <-req.reply
	if !reply.item || reply.index != 2 {
		t.Fatal("Enter ran an application action instead of opening the session", reply)
	}
}
func TestBrowserCompactListLabelKeepsFullDetailsAndSearch(t *testing.T) {
	const folder = "/home/pip/Documents/projects/devbox/rewrite"
	req := browserRequest()
	req.page.Collection.Items[0].Key = folder
	req.page.Collection.Items[0].Label = folder
	req.page.Collection.Items[0].ListLabel = "devbox/rewrite"
	req.page.Collection = snapshotCollection(req.page.Collection)
	req.itemKey = folder
	m := newTerminalModel(req, true)
	for _, width := range []int{28, 56} {
		for _, active := range []bool{true, false} {
			view := m.objectList(*req.page.Collection, folder, "", width, 12, active)
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "devbox/rewrite") || strings.Contains(plain, "/home/pip") {
				t.Fatalf("compact label missing (active=%t):\n%s", active, plain)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
		}
	}
	preview := ansi.Strip(m.browserContent(90, 20))
	title, _, _ := strings.Cut(preview, "\n")
	if strings.TrimSpace(title) != folder {
		t.Fatal("details lost full path", preview)
	}
	key(m, '/', "/")
	m.Update(tea.PasteMsg{Content: "/home/pip/Documents"})
	key(m, tea.KeyEnter, "")
	if matches := m.matches(); len(matches) != 1 || matches[0] != 0 {
		t.Fatal("full path no longer searchable", matches)
	}
	if view := ansi.Strip(m.objectList(*req.page.Collection, "", "", 56, 12, true)); !strings.Contains(view, "devbox/rewrite") {
		t.Fatal("filter changed the compact label", view)
	}
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; !reply.item || reply.index != 0 {
		t.Fatal("compact label changed dispatch", reply)
	}
}

func TestBrowserActionFocusAndShortcuts(t *testing.T) {
	req := browserRequest()
	m := newTerminalModel(req, false)
	key(m, tea.KeyDown, "")
	key(m, tea.KeyDown, "")
	key(m, tea.KeyRight, "")
	if reply := <-req.reply; !reply.item || reply.index != 2 {
		t.Fatal("Right opened application commands instead of this object's menu", reply)
	}
	req.itemKey = "session-b"
	m.load(req)
	if !m.objects || m.cursor != 2 {
		t.Fatal("returning to object list lost cursor")
	}
	key(m, 'n', "n")
	reply := <-req.reply
	if reply.item || reply.index != 0 || reply.focusItem != "session-b" {
		t.Fatal(reply)
	}
}
func TestBrowserFilterAndRefreshKeepStableIdentity(t *testing.T) {
	req := browserRequest()
	req.itemKey = "session-b"
	req.page.Collection.Items[0], req.page.Collection.Items[2] = req.page.Collection.Items[2], req.page.Collection.Items[0]
	m := newTerminalModel(req, false)
	if m.cursor != 0 {
		t.Fatal("refresh retained row number instead of object key")
	}
	key(m, '/', "/")
	m.Update(tea.PasteMsg{Content: "pi"})
	key(m, tea.KeyEnter, "")
	key(m, tea.KeyEnter, "")
	reply := <-req.reply
	if !reply.item || reply.index != 1 || reply.query != "pi" {
		t.Fatal(reply)
	}
}
func TestEmptyBrowserStartsOnCreate(t *testing.T) {
	req := browserRequest()
	req.page.Collection.Items = nil
	m := newTerminalModel(req, false)
	view := m.View().Content
	if m.objects || !strings.Contains(view, "No sessions yet.") || strings.Contains(view, "0 sessions") {
		t.Fatal(view)
	}
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; reply.item || reply.index != 0 {
		t.Fatal(reply)
	}
}
func TestObjectMenuKeepsNavigationAndDoesNotSwitchTabs(t *testing.T) {
	req := request(Action{Label: "Open"}, Action{Label: "Make folder default"})
	req.page.Navigation = &Navigation{Collection: browserFixture(), Key: "session-b"}
	req.page.Title = "Session · claude"
	m := newTerminalModel(req, false)
	m.width = 112
	view := m.View().Content
	if !strings.Contains(view, "claude *") || !strings.Contains(view, "Make folder default") {
		t.Fatal(view)
	}
	key(m, tea.KeyTab, "")
	select {
	case <-req.reply:
		t.Fatal("nested menu silently changed collections")
	default:
	}
	key(m, tea.KeyLeft, "")
	if reply := <-req.reply; !reply.back {
		t.Fatal(reply)
	}
}
func TestBrowserSnapshotsHaveNoWorkflowHandlers(t *testing.T) {
	c := browserFixture()
	c.Items[0].Open = func() error { return nil }
	checked := true
	c.Items[0].Summary = []Field{{Value: "automatic"}}
	c.Items[0].Actions[0].Run = func() (bool, error) { return true, nil }
	c.Items[0].Actions[0].Checked = &checked
	c.Items[0].Actions[0].Fields = []Field{{Values: []string{"original"}}}
	copy := snapshotCollection(&c)
	if copy.Items[0].Open != nil || c.Items[0].Open == nil {
		t.Fatal("snapshot retained handler or mutated original")
	}
	if copy.Items[0].Actions[0].Run != nil || c.Items[0].Actions[0].Run == nil {
		t.Fatal("preview snapshot retained an action handler or changed its owner")
	}
	*copy.Items[0].Actions[0].Checked = false
	copy.Items[0].Actions[0].Fields[0].Values[0] = "changed"
	copy.Items[0].Summary[0].Value = "changed"
	if !checked || c.Items[0].Actions[0].Fields[0].Values[0] != "original" || c.Items[0].Summary[0].Value != "automatic" {
		t.Fatal("preview actions or compact summary alias mutable workflow state")
	}
	copy.Items[0].Label = "changed"
	copy.Items[0].Fields[0].Value = "changed"
	if c.Items[0].Fields[0].Value == "changed" {
		t.Fatal("snapshot aliases workflow fields")
	}
	if c.Items[0].Label == "changed" {
		t.Fatal("snapshot aliases workflow collection")
	}
}
func TestRunnerScopesBrowserNavigationToNestedWorkflow(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("1\n0\n0\n"), &out)
	c := browserFixture()
	c.Items = c.Items[:1]
	c.Items[0].Open = func() error {
		if r.navigation == nil || r.navigation.Key != "/project" {
			t.Fatal("object context not passed to child")
		}
		return r.Run(func() (Screen, error) { return Screen{Title: "Folder", Back: "Back"}, nil })
	}
	if err := r.Run(func() (Screen, error) { return Screen{Title: "Sessions", Back: "Exit", Collection: &c}, nil }); err != nil {
		t.Fatal(err)
	}
	if r.navigation != nil {
		t.Fatal("closed browser leaked navigation into next workflow")
	}
}
func TestGroupedActionsScrollWithoutLosingFocusedRow(t *testing.T) {
	var actions []Action
	for _, label := range []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"} {
		actions = append(actions, Action{Label: label, BreakBefore: true})
	}
	req := request(actions...)
	m := newTerminalModel(req, false)
	m.width, m.height = 48, 20
	key(m, tea.KeyEnd, "")
	if !strings.Contains(m.View().Content, "▸ Ten") {
		t.Fatal("focused last action was clipped", m.View().Content)
	}
}
