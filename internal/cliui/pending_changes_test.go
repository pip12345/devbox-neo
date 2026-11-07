package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPendingChangeMarkerKeepsStateSelectionAndDispatch(t *testing.T) {
	for _, width := range []int{28, 38, 56} {
		for _, color := range []bool{false, true} {
			req := browserRequest()
			req.itemKey = "session-b"
			req.page.Collection.Items[2].PendingChanges = true
			req.page.Collection.Items[2].Status = "running"
			req.page.Collection.Items[2].Activity = "5 minutes ago"
			m := newTerminalModel(req, color)
			for _, active := range []bool{false, true} {
				view := m.objectList(*req.page.Collection, "session-b", "", width, 12, active)
				plain := ansi.Strip(view)
				for _, want := range []string{"claude * ↻", "running", "5 minutes ago"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("width %d lost %q: %s", width, want, plain)
					}
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("width %d overflow: %q", width, line)
					}
				}
				if color {
					focused := active
					if !strings.Contains(view, rowText(" ↻", tuiAmber, focused, true)) || !strings.Contains(view, rowText("running", tuiMint, focused, false)) {
						t.Fatal("pending marker changed runtime color or lost amber", view)
					}
				}
			}
			key(m, tea.KeyEnter, "")
			if reply := <-req.reply; !reply.item || reply.index != 2 {
				t.Fatal("marker changed dispatch", reply)
			}
		}
	}
}

func TestPendingChangeNoticeColorsOnlyHeading(t *testing.T) {
	fields := []Field{
		{Label: "Changes", Value: "↻ Recreate needed", Attention: true},
		{Label: "Reason", Value: "network changed"},
	}
	for _, color := range []bool{false, true} {
		req := request(Action{Label: "Continue"}, Action{Label: "Recreate"})
		m := newTerminalModel(req, color)
		view := fieldLines(fields, 60)
		if !strings.Contains(ansi.Strip(view), "↻ Recreate needed") || !strings.Contains(ansi.Strip(view), "network changed") {
			t.Fatal("notice not readable without color", view)
		}
		if color && (!strings.Contains(view, tint("↻ Recreate needed", tuiAmber)) || !strings.Contains(view, tint("network changed", tuiWhite))) {
			t.Fatal("notice or reason uses wrong color", view)
		}
		c := browserFixture()
		c.Items[2].Fields = fields
		c.Items[2].PendingChanges = true
		req.page.Collection = snapshotCollection(&c)
		req.itemKey = "session-b"
		m.load(req)
		if view := ansi.Strip(m.browserContent(60, 24)); !strings.Contains(view, "↻ Recreate needed") || !strings.Contains(view, "network changed") {
			t.Fatal("preview lost pending changes", view)
		}
		req.page.Collection = nil
		req.page.Fields = fields
		m.load(req)
		for _, size := range [][2]int{{48, 20}, {80, 24}, {112, 34}} {
			m.width, m.height = size[0], size[1]
			rendered := m.View().Content
			if !color && strings.Contains(rendered, "\x1b") {
				t.Fatal("no-color menu contains ANSI", rendered)
			}
			view := ansi.Strip(rendered)
			if !strings.Contains(view, "↻ Recreate needed") || !strings.Contains(view, "Continue") {
				t.Fatal("menu lost notice or access action", view)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("pending notice overflows menu", line)
				}
			}
		}
	}
}

func TestPlainBrowserPendingMarkerPreservesObjectIdentity(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("1\n0\n"), &out)
	opened := false
	if err := r.Run(func() (Screen, error) {
		return Screen{Title: "Sessions", Back: "Exit", Collection: &Collection{Items: []Item{{
			Key: "stable-id", Label: "work", PendingChanges: true,
			Open: func() error { opened = true; return nil },
		}}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !opened || !strings.Contains(out.String(), "work ↻") || strings.Contains(out.String(), "\x1b") {
		t.Fatal("plain pending marker lost dispatch or added ANSI", out.String())
	}
}
