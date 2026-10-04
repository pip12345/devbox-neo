package cliui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPageKeysNavigateDisplayedListsAndClampAtEndpoints(t *testing.T) {
	for _, objects := range []bool{false, true} {
		t.Run(fmt.Sprint(objects), func(t *testing.T) {
			req := request()
			for i := range 60 {
				label := fmt.Sprintf("match %02d", i)
				if i%2 != 0 {
					label = fmt.Sprintf("other %02d", i)
				}
				req.page.Actions = append(req.page.Actions, Action{Label: label})
			}
			if objects {
				req.page.Collection = &Collection{Title: "Sessions"}
				for i, a := range req.page.Actions {
					req.page.Collection.Items = append(req.page.Collection.Items, Item{Key: fmt.Sprint(i), Label: a.Label})
				}
			}
			req.query = "match"
			m := newTerminalModel(req, false)
			m.width, m.height = 80, 24
			// Filter to alternate rows; paging and dispatch must use that snapshot.
			m.query = "match"
			indices := m.matches()
			step := m.pageSize()
			if step <= 5 {
				t.Fatal("page navigation still uses a five-line detail step", step)
			}
			key(m, tea.KeyPgDown, "")
			if m.cursor != min(step, len(indices)-1) || m.scroll != 0 {
				t.Fatal("Page Down did not move the list", m.cursor, step)
			}
			for range 10 {
				key(m, tea.KeyPgDown, "")
			}
			if m.cursor != len(indices)-1 {
				t.Fatal("Page Down wrapped instead of clamping", m.cursor)
			}
			for range 10 {
				key(m, tea.KeyPgUp, "")
			}
			if m.cursor != 0 {
				t.Fatal("Page Up wrapped instead of clamping", m.cursor)
			}
			key(m, tea.KeyEnd, "")
			key(m, tea.KeyEnter, "")
			if reply := <-req.reply; reply.index != indices[len(indices)-1] || reply.item != objects {
				t.Fatal("paged selection dispatched a different row", reply)
			}
		})
	}
}

func TestReadOnlyViewsPageAndJumpToBothEnds(t *testing.T) {
	req := request()
	for i := range 200 {
		req.body += fmt.Sprintf("line %03d\n", i)
	}
	m := newTerminalModel(req, false)
	m.width, m.height = 80, 24
	key(m, tea.KeyPgDown, "")
	if m.scroll <= 5 || m.cursor != 0 {
		t.Fatal("read-only paging moved a selection or kept the old tiny step", m.scroll, m.cursor)
	}
	key(m, tea.KeyEnd, "")
	if !strings.Contains(m.View().Content, "line 199") {
		t.Fatal("End did not reach the bottom of the view", m.View().Content)
	}
	key(m, tea.KeyHome, "")
	if m.scroll != 0 || !strings.Contains(m.View().Content, "line 000") {
		t.Fatal("Home did not reach the top", m.scroll, m.View().Content)
	}
}

func TestShortcutsAreScreenLocalAndNeverExecuteDuringInput(t *testing.T) {
	for _, mode := range []string{"menu", "text", "filter", "busy", "confirmation"} {
		t.Run(mode, func(t *testing.T) {
			req := request(Action{Label: "Recreate", Shortcut: "r"})
			m := newTerminalModel(req, false)
			switch mode {
			case "text":
				req.input = true
				m.load(req)
			case "filter":
				key(m, '/', "/")
			case "busy":
				m.waiting = true
			case "confirmation":
				req = request(Action{Label: "No"}, Action{Label: "Yes", Danger: true})
				req.confirm = true
				m.load(req)
			}
			key(m, 'r', "r")
			select {
			case reply := <-req.reply:
				if mode != "menu" || reply.index != 0 {
					t.Fatal("shortcut escaped its screen/input scope", mode, reply)
				}
			default:
				if mode == "menu" {
					t.Fatal("menu shortcut did not dispatch")
				}
			}
			if mode == "text" || mode == "filter" {
				if m.input.Value() != "r" {
					t.Fatal("shortcut swallowed typed input", mode, m.input.Value())
				}
			}
		})
	}

	root := browserRequest()
	m := newTerminalModel(root, false)
	key(m, 'r', "r")
	if reply := <-root.reply; reply.index != 2 || reply.item {
		t.Fatal("browser r did not retain Refresh", reply)
	}
}

func TestUniformActionStylingAndShortcutColumns(t *testing.T) {
	for _, color := range []bool{false, true} {
		req := request(
			Action{Label: "Continue", Group: "Use", Shortcut: "c"},
			Action{Label: "Recreate", Group: "Container", Shortcut: "r"},
			Action{Label: "Delete", Group: "Manage", Danger: true},
		)
		m := newTerminalModel(req, color)
		m.width, m.height = 100, 30
		rows := m.actionList(90, 8, true)
		plain := ansi.Strip(rows)
		if strings.Contains(plain, "…") || strings.Contains(plain, "...") {
			t.Fatal("menu depth uses a punctuation suffix", plain)
		}
		columns := []int{}
		for _, line := range strings.Split(plain, "\n") {
			if index := strings.Index(line, "[c]"); index >= 0 {
				columns = append(columns, ansi.StringWidth(line[:index]))
			}
			if index := strings.Index(line, "[r]"); index >= 0 {
				columns = append(columns, ansi.StringWidth(line[:index]))
			}
		}
		if len(columns) != 2 || columns[0] != columns[1] || columns[0] >= 60 {
			t.Fatal("frequent shortcuts are not aligned beside the actions", columns, plain)
		}
		for _, line := range strings.Split(ansi.Strip(m.actionList(180, 8, true)), "\n") {
			if index := strings.Index(line, "[c]"); index >= 0 && ansi.StringWidth(line[:index]) != columns[0] {
				t.Fatal("a wider pane moved the shortcut away from the actions", line)
			}
		}
		italic := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*3(?:;[0-9]+)*m`)
		if italic.MatchString(rows) {
			t.Fatal("menu depth still changes font style", rows)
		}
		styledLabels := regexp.MustCompile(`\x1b\[([0-9;]+)m(?:Continue|Recreate|Delete)`).FindAllStringSubmatch(m.actionList(90, 8, false), -1)
		if len(styledLabels) != 3 {
			t.Fatal("missing action-label styles", rows)
		}
		for _, label := range styledLabels {
			if !slices.Contains(strings.Split(label[1], ";"), "1") {
				t.Fatal("menu depth still changes action-label boldness", label)
			}
		}
		if styledLabels[0][1] != styledLabels[1][1] || styledLabels[1][1] == styledLabels[2][1] {
			t.Fatal("ordinary actions differ or danger highlighting disappeared", styledLabels)
		}
		if strings.Contains(plain, "menu") {
			t.Fatal("action list still has menu-depth hints", plain)
		}
		if !color && strings.Contains(m.View().Content, "\x1b[") {
			t.Fatal("no-color output contains styling", m.View().Content)
		}
	}
}

func TestCompactGroupsKeepTheirContextWhenScrolledOrFiltered(t *testing.T) {
	req := request()
	for i := range 20 {
		req.page.Actions = append(req.page.Actions, Action{Label: fmt.Sprintf("Command %02d", i), Group: "Use"})
	}
	m := newTerminalModel(req, false)
	m.cursor = 10
	text := ansi.Strip(m.actionList(60, 8, true))
	if !strings.Contains(text, "Use") || !strings.Contains(text, "▸ Command 10") || !strings.Contains(text, "↑ more") || !strings.Contains(text, "↓ more") {
		t.Fatal("scrolled group lost its label, selection or continuation hints", text)
	}
	if strings.Contains(text, "Use\n") {
		t.Fatal("group label still consumes a separate row", text)
	}
	m.query, m.cursor = "Command 19", 0
	if text = ansi.Strip(m.actionList(60, 8, true)); !strings.Contains(text, "Use") || !strings.Contains(text, "Command 19") {
		t.Fatal("filtered group lost its context", text)
	}
}

func TestCategorySpacingIsOneVisualRowAndNeverAnAction(t *testing.T) {
	req := request(
		Action{Label: "Continue", Group: "Use"},
		Action{Label: "Open", Group: "Use"},
		Action{Label: "Status", Group: "Inspect", BreakBefore: true},
		Action{Label: "Logs", Group: "Inspect"},
	)
	indices := []int{0, 1, 2, 3}
	if rows := actionRows(req.page.Actions, indices); !slices.Equal(rows, []int{0, 1, -1, 2, 3}) {
		t.Fatal("category boundary and explicit break created missing or duplicate gaps", rows)
	}
	m := newTerminalModel(req, false)
	lines := strings.Split(ansi.Strip(m.actionList(70, 8, true)), "\n")
	if strings.TrimSpace(lines[2]) != "" || !strings.Contains(lines[3], "Inspect") || !strings.Contains(lines[3], "Status") {
		t.Fatal("category boundary has no breathing room", lines)
	}
	m.cursor = 3
	if text := ansi.Strip(m.actionList(70, 2, true)); !strings.Contains(text, "Inspect") || !strings.Contains(text, "▸ Logs") {
		t.Fatal("scrolled category lost its label or focused action", text)
	}
	m.query, m.cursor = "Status", 0
	text := ansi.Strip(m.actionList(70, 8, true))
	if !strings.HasPrefix(text, "Inspect") || !strings.Contains(text, "Status") {
		t.Fatal("filter introduced a leading category gap", text)
	}
}

func TestPagingCountsCategoryGapsAndDispatchesTheDisplayedAction(t *testing.T) {
	req := request()
	for i := range 60 {
		req.page.Actions = append(req.page.Actions, Action{Label: fmt.Sprintf("Action %02d", i), Group: fmt.Sprintf("Group %d", i/3)})
	}
	m := newTerminalModel(req, false)
	m.width, m.height = 80, 24
	indices := m.matches()
	rows := actionRows(req.page.Actions, indices)
	step := m.pageSize()
	target := step
	if rows[target] < 0 {
		target++
	}
	key(m, tea.KeyPgDown, "")
	if indices[m.cursor] != rows[target] {
		t.Fatal("paging ignored the displayed category gaps", m.cursor, step, rows)
	}
	key(m, tea.KeyEnter, "")
	if reply := <-req.reply; reply.index != rows[target] {
		t.Fatal("paging dispatched a spacer or another action", reply)
	}

	// A pasted filter can reduce the displayed rows before another navigation key.
	req.query = "Action 59"
	m.load(req)
	m.cursor = 20
	key(m, tea.KeyPgDown, "")
	if m.cursor != 0 {
		t.Fatal("paging retained an out-of-range filtered cursor", m.cursor)
	}
}

func TestDetailPagingRemainsIndependentOfMenuSelection(t *testing.T) {
	req := request(Action{Label: "Mounts", Fields: []Field{{Value: strings.Repeat("mount value\n", 30)}}})
	m := newTerminalModel(req, false)
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModCtrl})
	if m.scroll == 0 || m.cursor != 0 {
		t.Fatal("detail paging moved the selection or did not scroll", m.scroll, m.cursor)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModCtrl})
	if m.scroll != 0 {
		t.Fatal("detail paging did not return to the top", m.scroll)
	}
	select {
	case reply := <-req.reply:
		t.Fatal("detail scrolling submitted an action", reply)
	default:
	}
}

func TestUnmatchedBrowserFilterDoesNotShowAnEmptyInventoryOrAnotherTarget(t *testing.T) {
	req := browserRequest()
	req.query = "does not exist"
	m := newTerminalModel(req, false)
	text := ansi.Strip(m.browserContent(70, 24))
	if !strings.Contains(text, "No matches") || strings.Contains(text, "No sessions yet") || strings.Contains(text, "Continue") {
		t.Fatal("no-match preview describes a different state or target", text)
	}
	key(m, tea.KeyRight, "")
	select {
	case reply := <-req.reply:
		t.Fatal("Right opened an object excluded by the filter", reply)
	default:
	}
}

func TestBrowserPreviewFollowsObjectWithoutRunningItsActions(t *testing.T) {
	req := browserRequest()
	called := false
	req.page.Collection.Items[1].Actions[0].Run = func() (bool, error) { called = true; return false, nil }
	req.page.Collection = snapshotCollection(req.page.Collection)
	m := newTerminalModel(req, false)
	folder := ansi.Strip(m.browserContent(70, 24))
	if !strings.Contains(folder, "Create session here") || strings.Contains(folder, "Recreate") {
		t.Fatal("folder preview shows session commands", folder)
	}
	key(m, tea.KeyDown, "")
	session := ansi.Strip(m.browserContent(70, 24))
	if !strings.Contains(session, "Continue") || !strings.Contains(session, "Recreate") || strings.Contains(session, "Create session here") || strings.Contains(session, "All-session operations") {
		t.Fatal("session preview shows another scope's commands", session)
	}
	if called {
		t.Fatal("highlighting an object executed its preview action")
	}
	select {
	case reply := <-req.reply:
		t.Fatal("highlighting an object submitted it", reply)
	default:
	}
}

func TestSessionSummaryRetainsLifetimeInNarrowLayouts(t *testing.T) {
	req := request(Action{Label: "Continue", Shortcut: "c"})
	req.page.Title = "Session · Main"
	req.page.Summary = []Field{{Value: "pi"}, {Value: "running", Status: true}, {Value: "until stop"}}
	m := newTerminalModel(req, false)
	m.width, m.height = 48, 20
	text := m.View().Content
	if !strings.Contains(text, "running") || !strings.Contains(text, "until stop") || !strings.Contains(text, "▸ Continue") {
		t.Fatal("compact session context hid lifetime or the first action", text)
	}
}

func TestPageKeysCannotChangeOrApproveConfirmation(t *testing.T) {
	req := request(Action{Label: "No"}, Action{Label: "Yes", Danger: true})
	req.confirm = true
	m := newTerminalModel(req, false)
	key(m, tea.KeyPgDown, "")
	if m.cursor != 0 {
		t.Fatal("paging selected the destructive choice")
	}
	select {
	case reply := <-req.reply:
		t.Fatal("paging approved a confirmation", reply)
	default:
	}
}
