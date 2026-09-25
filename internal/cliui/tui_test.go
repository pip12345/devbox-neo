package cliui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(m *terminalModel, code rune, text string) { m.Update(tea.KeyPressMsg{Code: code, Text: text}) }
func request(actions ...Action) *screenRequest {
	return &screenRequest{page: Screen{Title: "Workspace", Back: "Back", Actions: actions}, reply: make(chan screenReply, 1), cursor: -1}
}
func TestNativeSelectionUsesFilteredSnapshot(t *testing.T) {
	req := request(Action{Label: "first"}, Action{Label: "second", Description: "api"}, Action{Label: "third", Description: "api"})
	m := newTerminalModel(req, false)
	key(m, '/', "/")
	for _, r := range "api" {
		key(m, r, string(r))
	}
	key(m, tea.KeyEnter, "")
	key(m, tea.KeyDown, "")
	key(m, tea.KeyEnter, "")
	reply := <-req.reply
	if reply.index != 2 || reply.query != "api" {
		t.Fatal("filter changed action identity", reply)
	}
	key(m, tea.KeyEnter, "")
	select {
	case <-req.reply:
		t.Fatal("action dispatched twice while workflow was busy")
	default:
	}
}
func TestNativeSearchAcceptsBracketedPaste(t *testing.T) {
	req := request(Action{Label: "api"}, Action{Label: "frontend"})
	m := newTerminalModel(req, false)
	key(m, '/', "/")
	m.Update(tea.PasteMsg{Content: "front"})
	key(m, tea.KeyEnter, "")
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; reply.index != 1 || reply.query != "front" {
		t.Fatal("pasted search was lost", reply)
	}
}

func TestNativeTabAndBackAreNavigationNotActions(t *testing.T) {
	req := request(Action{Label: "one"})
	req.canTab = true
	m := newTerminalModel(req, false)
	key(m, tea.KeyTab, "")
	if reply := <-req.reply; !reply.tab {
		t.Fatal(reply)
	}
	next := request()
	m.load(next)
	key(m, tea.KeyEscape, "")
	if reply := <-next.reply; !reply.back || reply.index != -1 {
		t.Fatal(reply)
	}
}
func TestNativeTextPreservesArgumentsAndCancelDoesNotSubmit(t *testing.T) {
	req := request()
	req.input = true
	req.prompt = "Exact argument"
	m := newTerminalModel(req, false)
	for _, r := range ":back" {
		key(m, r, string(r))
	}
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; reply.value != ":back" || reply.back {
		t.Fatal("native text treated a literal argument as a control", reply)
	}
	next := request()
	next.input = true
	m.load(next)
	key(m, 'q', "q")
	key(m, tea.KeyEscape, "")
	if reply := <-next.reply; !reply.back || reply.value != "" {
		t.Fatal("cancel submitted partial input", reply)
	}
}
func TestNativeConfirmationDefaultsToNoAndRequiresEnter(t *testing.T) {
	req := request(Action{Label: "No"}, Action{Label: "Yes", Danger: true})
	req.confirm = true
	m := newTerminalModel(req, false)
	key(m, 'y', "y")
	select {
	case <-req.reply:
		t.Fatal("unsubmitted affirmative confirmed operation")
	default:
	}
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; reply.index != 0 {
		t.Fatal("confirmation did not default to no", reply)
	}
	next := request(Action{Label: "No"}, Action{Label: "Yes", Danger: true})
	next.confirm = true
	m.load(next)
	key(m, tea.KeyDown, "")
	key(m, tea.KeyEnter, "")
	if reply := <-next.reply; reply.index != 1 {
		t.Fatal(reply)
	}
}
func TestNativeCancelAlsoCancelsBusyWorkflow(t *testing.T) {
	req := request()
	m := newTerminalModel(req, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancel = cancel
	m.waiting = true
	key(m, 'c', "") // No implicit action while busy.
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if ctx.Err() != context.Canceled {
		t.Fatal("Ctrl-C did not cancel command work")
	}
}
func TestNativeScreensFitAndKeepControls(t *testing.T) {
	for _, size := range [][2]int{{48, 20}, {80, 24}, {112, 34}, {160, 44}} {
		for _, mode := range []string{"list", "view", "input", "confirm", "busy", "browser", "object-menu", "empty-browser"} {
			req := request(Action{Label: "A long configuration choice", Description: strings.Repeat("wide value ", 15), Detail: strings.Repeat("Detailed context\n", 80)})
			req.body = "Folder: /work/目录\nCombining: e\u0301"
			req.notice = "First warning\nSecond warning"
			switch mode {
			case "browser", "object-menu", "empty-browser":
				collection := browserFixture()
				if mode == "object-menu" {
					req.page.Navigation = &Navigation{Collection: collection, Key: "session-b"}
				} else {
					if mode == "empty-browser" {
						collection.Items = nil
					}
					req.page.Collection = &collection
					req.canTab = true
				}
			case "view":
				req.page.Actions = nil
			case "input":
				req.input = true
				req.prompt = "Enter a new configuration value (Esc cancels): "
			case "confirm":
				req.confirm = true
				req.prompt = strings.Repeat("Delete target\n", 40)
			}
			m := newTerminalModel(req, true)
			m.width, m.height = size[0], size[1]
			m.waiting = mode == "busy"
			view := m.View()
			if !view.AltScreen {
				t.Fatal("native view lacks alternate screen")
			}
			lines := strings.Split(view.Content, "\n")
			if len(lines) > m.height {
				t.Fatalf("%s %v: too many rows: %d", mode, size, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%s %v: row overflow %q", mode, size, ansi.Strip(line))
				}
			}
			if !strings.Contains(ansi.Strip(view.Content), "Ctrl-C") {
				t.Fatalf("%s %v lost navigation", mode, size)
			}
		}
	}
}
func TestNativeSelectedValuesAndNoColor(t *testing.T) {
	checked := true
	req := request(Action{Label: "First"}, Action{Label: "Current", Selected: true, Checked: &checked})
	m := newTerminalModel(req, false)
	if m.cursor != 1 {
		t.Fatal("current selection was not focused")
	}
	view := m.View().Content
	if strings.Contains(view, "\x1b[") || !strings.Contains(view, "✓") || !strings.Contains(view, "*") {
		t.Fatal("no-color output lost state markers", view)
	}
}
