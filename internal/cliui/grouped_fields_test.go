package cliui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestGroupedFieldsUseSemanticColorsAndSectionAlignment(t *testing.T) {
	fields := []Field{
		{Group: "Changes", Label: "Result", Value: "No changes", Success: true},
		{Group: "Changes", Label: "Reason", Value: "network changed"},
		{Group: "Pending", Label: "Result", Value: "Recreate needed", Attention: true},
		{Group: "Problems", Label: "Error", Value: "invalid config", Warning: true},
		{Group: "Runtime", Label: "Container ID", Value: "container-id"},
	}
	view := fieldLines(fields, 60)
	for _, colored := range []string{strong("Changes", tuiWhite), tint("No changes", tuiMint), tint("Recreate needed", tuiAmber), tint("invalid config", tuiRed), tint("network changed", tuiWhite)} {
		if !strings.Contains(view, colored) {
			t.Fatal("field lost its semantic color", colored, view)
		}
	}
	plain := ansi.Strip(view)
	if strings.Count(plain, "Changes\n") != 1 || !strings.Contains(plain, "Result  No changes") || !strings.Contains(plain, "\n\nRuntime\nContainer ID  container-id") {
		t.Fatal("sections are repeated, unseparated, or aligned across unrelated groups", plain)
	}
}

func TestGroupedReadOnlyScreensFitAndScrollWithoutColor(t *testing.T) {
	var reasons []string
	for i := 0; i < 50; i++ {
		reasons = append(reasons, fmt.Sprintf("reason %02d: %s", i, strings.Repeat("wide-value ", 8)))
	}
	for _, size := range [][2]int{{48, 20}, {80, 24}, {112, 34}, {160, 44}} {
		for _, color := range []bool{false, true} {
			req := request()
			req.page.Title = "Status · Main"
			req.page.Fields = []Field{
				{Group: "Changes", Label: "Result", Value: "Recreate needed", Attention: true},
				{Group: "Changes", Label: "Container", Values: reasons},
				{Group: "Next step", Label: "Command", Value: "dbx --home '/home/custom home' recreate dbx-api-111111111111.main"},
				{Group: "Active commands", Label: "Count", Value: "0"},
			}
			if size[0] >= 112 {
				req.page.Navigation = &Navigation{Collection: browserFixture(), Key: "session-b"}
			}
			m := newTerminalModel(req, color)
			m.width, m.height = size[0], size[1]
			checkView := func() string {
				t.Helper()
				view := m.View().Content
				if !color && strings.Contains(view, "\x1b") {
					t.Fatal("no-color view contains escapes", view)
				}
				if len(strings.Split(view, "\n")) > size[1] {
					t.Fatal("grouped status exceeds terminal height", ansi.Strip(view))
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("grouped status exceeds terminal width", ansi.Strip(line))
					}
				}
				return ansi.Strip(view)
			}
			if view := checkView(); !strings.Contains(view, "Changes") || !strings.Contains(view, "Recreate needed") {
				t.Fatal("status hides its result", view)
			}
			key(m, tea.KeyPgDown, "")
			if m.scroll == 0 || m.cursor != 0 {
				t.Fatal("read-only paging moved selection or did not scroll", m.scroll, m.cursor)
			}
			checkView()
			key(m, tea.KeyEnd, "")
			if view := checkView(); !strings.Contains(view, "Active commands") || !strings.Contains(view, "Count") {
				t.Fatal("end of structured status is unreachable", view)
			}
			key(m, tea.KeyHome, "")
			if view := checkView(); m.scroll != 0 || !strings.Contains(view, "Recreate needed") {
				t.Fatal("Home lost the result", view)
			}
		}
	}
}

func TestPlainGroupedFieldsRemainReadableAndEscapeMetadata(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("0\n"), &out)
	if err := r.Run(func() (Screen, error) {
		return Screen{Title: "Status", Back: "Back", Fields: []Field{
			{Group: "Changes", Label: "Result", Value: "No changes", Success: true},
			{Group: "Changes", Label: "Runtime", Values: []string{"first reason", "second reason"}},
			{Group: "Session", Label: "Folder", Value: "/project"},
		}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Count(text, "\nChanges\n") != 1 || !strings.Contains(text, "\nSession\nFolder: /project") || !strings.Contains(text, "second reason") || strings.Contains(text, "\x1b") {
		t.Fatal("plain status lost structure or added color", text)
	}
	view := ansi.Strip(fieldLines([]Field{{Group: "Untrusted\x1b[31m", Label: "Reason", Values: []string{"value\nforged: row"}}}, 80))
	if !strings.Contains(view, `\x1b[31m`) || !strings.Contains(view, `value\nforged: row`) {
		t.Fatal("metadata controlled the terminal or forged a field", view)
	}
}
