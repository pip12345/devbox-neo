package cliui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPaneFocusPreservesHeaderAndContentPositions(t *testing.T) {
	for _, width := range []int{48, 80, 112, 170} {
		for _, color := range []bool{false, true} {
			browser := browserRequest()
			item := &browser.page.Collection.Items[2]
			item.Label = "main"
			item.Fields = []Field{
				{Label: "Changes", Value: "↻ Update pending", Attention: true},
				{Label: "Reason", Value: "bundled runtime guidance changed"},
				{Label: "Folder", Value: "/work/project"},
				{Label: "Last active", Value: "30 minutes ago"},
			}
			item.Summary = []Field{{Value: "pi"}, {Value: "stopped", Status: true}, {Value: "automatic"}}
			item.Actions[0].Group = "Use"
			item.Actions[0].Description = "Resume the harness conversation"
			item.Actions[1].Group = "Container"
			browser.itemKey = item.Key
			focused := request(item.Actions...)
			focused.page.Title, focused.page.Fields, focused.page.Summary = item.Label, item.Fields, item.Summary
			focused.page.Navigation = &Navigation{Collection: *browser.page.Collection, Key: item.Key}
			left, right := newTerminalModel(browser, color), newTerminalModel(focused, color)
			left.width, right.width, left.height, right.height = width, width, 48, 48
			leftHeader, rightHeader := ansi.Strip(left.header(width-4)), ansi.Strip(right.header(width-4))
			if len(strings.Split(leftHeader, "\n")) != len(strings.Split(rightHeader, "\n")) {
				t.Fatalf("width %d: focus changes header height:\n%q\n%q", width, leftHeader, rightHeader)
			}
			if strings.Contains(rightHeader, "[r] Refresh") || strings.Contains(rightHeader, "Tab ↔") {
				t.Fatal("inactive browser shortcuts advertised in the action pane", rightHeader)
			}
			if width < 112 {
				continue
			}
			leftView, rightView := ansi.Strip(left.View().Content), ansi.Strip(right.View().Content)
			for _, text := range []string{"Sessions", "main · pi", "Changes", "Reason", "Folder", "Last active  30", "Continue", "Recreate"} {
				find := func(view string) int {
					for i, line := range strings.Split(view, "\n") {
						if strings.Contains(line, text) {
							return i
						}
					}
					return -1
				}
				if a, b := find(leftView), find(rightView); a < 0 || a != b {
					t.Fatalf("width %d: %q moved from row %d to %d:\n%s\n%s", width, text, a, b, leftView, rightView)
				}
			}
		}
	}
}
