package cliui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactSessionLayoutsFitAndKeepContextAndShortcuts(t *testing.T) {
	for _, size := range [][2]int{{48, 20}, {80, 24}, {112, 34}, {160, 44}, {200, 48}} {
		for _, color := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/color=%t", size[0], size[1], color), func(t *testing.T) {
				req := request(
					Action{Label: "Continue", Group: "Use", Shortcut: "c"},
					Action{Label: "Open", Group: "Use", Shortcut: "o"},
					Action{Label: "Open with options", Group: "Use"},
					Action{Label: "Shell", Group: "Use", Shortcut: "s"},
					Action{Label: "Exec", Group: "Use", Shortcut: "e"},
					Action{Label: "SSH", Group: "Use"},
					Action{Label: "Status", Group: "Inspect", Shortcut: "i"},
					Action{Label: "Logs", Group: "Inspect", Shortcut: "l"},
					Action{Label: "Networks", Group: "Inspect"},
					Action{Label: "Start", Group: "Container"},
					Action{Label: "Stop", Group: "Container"},
					Action{Label: "Recreate", Group: "Container", Shortcut: "r"},
					Action{Label: "Selected configs", Group: "Manage"},
					Action{Label: "Make folder default", Group: "Manage"},
					Action{Label: "Copy or move", Group: "Manage"},
					Action{Label: "Rename session", Group: "Manage"},
					Action{Label: "Change workspace", Group: "Manage"},
					Action{Label: "Delete", Group: "Manage", Danger: true},
				)
				req.page.Title = "Session · Main"
				req.page.Fields = []Field{{Label: "Folder", Value: "/work/project"}}
				req.page.Summary = []Field{{Value: "pi"}, {Value: "stopped", Status: true}, {Value: "automatic"}}
				req.page.Actions[0].Description = "Resume the recorded harness conversation"
				navigation := browserFixture()
				navigation.Items[2].Label = "Main"
				req.page.Navigation = &Navigation{Collection: navigation, Key: "session-b"}
				m := newTerminalModel(req, color)
				m.width, m.height = size[0], size[1]
				text := ansi.Strip(m.View().Content)
				for _, want := range []string{"Session · Main", "automatic", "/work/project", "▸ Continue", "[c]", "Ctrl-C"} {
					if !strings.Contains(text, want) {
						t.Fatal("session layout lost essential context or controls", want, text)
					}
				}
				if len(strings.Split(text, "\n")) > size[1] {
					t.Fatal("session view exceeds terminal height", text)
				}
				for _, line := range strings.Split(text, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("session view exceeds terminal width", line)
					}
				}
				if size == [2]int{112, 34} && color {
					t.Log("wide session layout:\n" + text)
				}
			})
		}
	}
}
