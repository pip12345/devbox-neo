package cliui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(m *terminalModel, code rune, text string) { m.Update(tea.KeyPressMsg{Code: code, Text: text}) }
func request(actions ...Action) *screenRequest {
	return &screenRequest{page: Screen{Title: "Workspace", Back: "Back", Actions: actions}, reply: make(chan screenReply, 1), cursor: -1}
}
func TestNativeSelectionWrapsWithinDisplayedList(t *testing.T) {
	for _, mode := range []string{"actions", "objects", "filtered-actions", "filtered-objects", "single", "confirmation"} {
		for _, vim := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/vim=%t", mode, vim), func(t *testing.T) {
				req := request(Action{Label: "match first"}, Action{Label: "other"}, Action{Label: "match last"})
				switch mode {
				case "objects", "filtered-objects":
					req.page.Collection = &Collection{Items: []Item{{Key: "first", Label: "match first"}, {Key: "other", Label: "other"}, {Key: "last", Label: "match last"}}}
				case "single":
					req.page.Actions = req.page.Actions[:1]
				case "confirmation":
					req.page.Actions = []Action{{Label: "No"}, {Label: "Yes"}}
					req.confirm = true
				}
				if strings.HasPrefix(mode, "filtered-") {
					req.query = "match"
				}
				m := newTerminalModel(req, false)
				up, down := rune(tea.KeyUp), rune(tea.KeyDown)
				upText, downText := "", ""
				if vim {
					up, down, upText, downText = 'k', 'j', "k", "j"
				}
				indices := m.matches()
				m.scroll = 4
				key(m, up, upText)
				if m.cursor != len(indices)-1 || m.scroll != 0 {
					t.Fatal("up did not wrap to the last displayed choice", m.cursor, m.scroll)
				}
				key(m, down, downText)
				if m.cursor != 0 {
					t.Fatal("down did not wrap to the first displayed choice", m.cursor)
				}
				key(m, up, upText)
				select {
				case <-req.reply:
					t.Fatal("navigation submitted a choice without Enter")
				default:
				}
				key(m, tea.KeyEnter, "")
				if reply := <-req.reply; reply.index != indices[len(indices)-1] || reply.item != m.objects {
					t.Fatal("wrapped selection dispatched the wrong item", reply)
				}
			})
		}
	}
}

func TestNativeEmptySelectionStillScrollsDetails(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		req := request()
		if filtered {
			req.page.Actions = []Action{{Label: "other"}}
			req.query = "no matches"
		}
		m := newTerminalModel(req, false)
		key(m, tea.KeyUp, "")
		if m.scroll != 0 {
			t.Fatal("details scrolled above the top", m.scroll)
		}
		key(m, tea.KeyDown, "")
		if m.scroll != 1 || m.cursor != 0 {
			t.Fatal("empty selection stopped scrolling", m.scroll, m.cursor)
		}
	}
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
func TestActionValueColumnsIncludeSelectionMarkers(t *testing.T) {
	for _, checked := range []bool{false, true} {
		req := request(Action{Label: "Make folder default", Value: "Replaces Main", Checked: &checked}, Action{Label: "Selected", Value: "value", Selected: true})
		m := newTerminalModel(req, false)
		text := ansi.Strip(m.actionList(90, 10, true))
		if !strings.Contains(text, "Make folder default  Replaces Main") || !strings.Contains(text, "* Selected") || strings.Contains(text, "…") {
			t.Fatal("markers clipped a label despite sufficient width", text)
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
