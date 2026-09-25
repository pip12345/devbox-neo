package cliui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type screenRequest struct {
	page                   Screen
	body, notice, prompt   string
	input, confirm, canTab bool
	cursor                 int
	itemKey, query         string
	reply                  chan screenReply
}
type screenReply struct {
	index                   int
	item, tab, back         bool
	value, query, focusItem string
	err                     error
}
type workingMsg struct{}
type pulseMsg time.Time

func pulse() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg { return pulseMsg(t) })
}

type terminalModel struct {
	req                                  *screenRequest
	width, height, cursor, clock, scroll int
	query, objectKey                     string
	objects, searching, waiting, color   bool
	input                                textinput.Model
	err                                  error
	cancel                               context.CancelFunc
}

func newTerminalModel(req *screenRequest, color bool) *terminalModel {
	m := &terminalModel{width: 100, height: 30, color: color}
	m.load(req)
	return m
}
func (m *terminalModel) load(req *screenRequest) tea.Cmd {
	m.req = req
	m.waiting, m.searching = false, false
	m.scroll, m.cursor = 0, 0
	m.query = req.query
	m.input = textinput.New()
	m.input.Prompt = "› "
	m.input.SetWidth(max(10, m.width-16))
	m.objects = req.page.Collection != nil && len(req.page.Collection.Items) > 0 && (req.cursor < 0 || req.page.FocusItem != "")
	target := req.itemKey
	if req.page.FocusItem != "" {
		target = req.page.FocusItem
		m.query = ""
	}
	m.objectKey = target
	if m.objectKey == "" && req.page.Collection != nil && len(req.page.Collection.Items) > 0 {
		m.objectKey = req.page.Collection.Items[0].Key
	}
	// An executed action may change its own label (for example Set to Clear).
	// Do not strand the user in an empty action filter after that change.
	if !m.objects && req.cursor >= 0 && m.query != "" && len(m.matches()) == 0 {
		m.query = ""
	}
	for pos, index := range m.matches() {
		if m.objects {
			if req.page.Collection.Items[index].Key == target {
				m.cursor = pos
				break
			}
		} else if index == req.cursor || req.cursor < 0 && req.page.Actions[index].Selected {
			m.cursor = pos
			break
		}
	}
	if req.input {
		return m.input.Focus()
	}
	return nil
}
func (m *terminalModel) Init() tea.Cmd {
	if m.req.input {
		return tea.Batch(pulse(), m.input.Focus())
	}
	return pulse()
}
func (m *terminalModel) matches() []int {
	var indices []int
	if m.req == nil {
		return indices
	}
	query := strings.ToLower(m.query)
	if m.objects {
		return matchingItems(*m.req.page.Collection, query)
	} else {
		for i, a := range m.req.page.Actions {
			if query == "" || strings.Contains(strings.ToLower(a.Label+" "+a.Value+" "+a.Description+" "+a.Detail), query) {
				indices = append(indices, i)
			}
		}
	}
	return indices
}
func matchingItems(c Collection, query string) []int {
	var indices []int
	query = strings.ToLower(query)
	for i, item := range c.Items {
		if query == "" || strings.Contains(strings.ToLower(item.Label+" "+item.Description+" "+item.Key), query) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *terminalModel) respond(r screenReply) {
	if m.waiting {
		return
	}
	r.query = m.query
	if item := m.currentObject(); item != nil {
		r.focusItem = item.Key
	}
	m.req.reply <- r
	m.waiting = true
}
func (m *terminalModel) currentObject() *Item {
	c := m.req.page.Collection
	if c == nil {
		return nil
	}
	if m.objects {
		indices := m.matches()
		if len(indices) > 0 {
			return &c.Items[indices[min(m.cursor, len(indices)-1)]]
		}
		return nil
	}
	for i := range c.Items {
		if c.Items[i].Key == m.objectKey {
			return &c.Items[i]
		}
	}
	return nil
}
func (m *terminalModel) focusObjects(objects bool) {
	if item := m.currentObject(); item != nil {
		m.objectKey = item.Key
	}
	m.objects = objects
	m.cursor, m.scroll = 0, 0
	m.query = ""
	if objects {
		for pos, index := range m.matches() {
			if m.req.page.Collection.Items[index].Key == m.objectKey {
				m.cursor = pos
				break
			}
		}
	}
}
func (m *terminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workingMsg:
		m.waiting = true
	case *screenRequest:
		return m, m.load(msg)
	case *terminalHandoff:
		return m, tea.Exec(msg, func(err error) tea.Msg {
			if err != nil {
				select {
				case msg.ready <- err:
				default:
				}
			}
			return handoffEnded{err}
		})
	case handoffEnded:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, m.width-16))
	case pulseMsg:
		m.clock++
		return m, pulse()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.err = context.Canceled
			m.respond(screenReply{err: context.Canceled})
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.waiting {
			return m, nil
		}
		if m.req.input {
			switch key {
			case "esc":
				m.respond(screenReply{index: -1, back: true})
			case "enter":
				m.respond(screenReply{value: m.input.Value()})
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		if m.searching {
			switch key {
			case "esc":
				m.query = ""
				m.searching = false
				m.input.Blur()
			case "enter":
				m.searching = false
				m.input.Blur()
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.query = m.input.Value()
				m.cursor = 0
				return m, cmd
			}
			return m, nil
		}
		indices := m.matches()
		switch key {
		case "esc", "q":
			if m.query != "" {
				m.query = ""
				m.cursor = 0
			} else if m.req.page.Collection != nil && !m.objects && len(m.req.page.Collection.Items) > 0 {
				m.focusObjects(true)
			} else {
				m.respond(screenReply{index: -1, back: true})
			}
		case "left":
			if m.req.confirm {
				m.cursor = 0
			} else if m.req.page.Collection != nil && len(m.req.page.Collection.Items) > 0 {
				m.focusObjects(true)
			} else if m.req.page.Navigation != nil {
				m.respond(screenReply{index: -1, back: true})
			}
		case "right":
			if m.req.confirm {
				m.cursor = 1
			} else if m.req.page.Collection != nil {
				m.focusObjects(false)
			}
		case "up", "k":
			if len(indices) == 0 {
				m.scroll = max(0, m.scroll-1)
			} else {
				m.cursor = max(0, m.cursor-1)
				m.scroll = 0
			}
		case "down", "j":
			if len(indices) == 0 {
				m.scroll++
			} else {
				m.cursor = min(max(0, len(indices)-1), m.cursor+1)
				m.scroll = 0
			}
		case "pgdown":
			m.scroll += 5
		case "pgup":
			m.scroll = max(0, m.scroll-5)
		case "home":
			m.cursor = 0
			m.scroll = 0
		case "end":
			m.cursor = max(0, len(indices)-1)
			m.scroll = 0
		case "tab":
			if m.req.canTab {
				m.respond(screenReply{tab: true})
			}
		case "/":
			if len(indices) > 0 && !m.req.confirm {
				m.searching = true
				m.input.SetValue(m.query)
				return m, m.input.Focus()
			}
		case "enter":
			if len(indices) > 0 {
				m.respond(screenReply{index: indices[min(m.cursor, len(indices)-1)], item: m.objects})
			} else if !m.objects && len(m.req.page.Actions) == 0 {
				m.respond(screenReply{index: -1, back: true})
			}
		default:
			for i, a := range m.req.page.Actions {
				if a.Shortcut != "" && a.Shortcut == key {
					m.query = ""
					m.respond(screenReply{index: i})
					break
				}
			}
		}
	default:
		if m.req.input || m.searching {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if m.searching {
				m.query = m.input.Value()
				m.cursor = 0
			}
			return m, cmd
		}
	}
	return m, nil
}

const tuiLine = "#42536A"
const tuiMint = "#9FF5CE"
const tuiMuted = "#8B9DB4"
const tuiWhite = "#E4EDF7"
const tuiRed = "#F38BA8"
const tuiAmber = "#E9C46A"

func tint(s, color string) string { return lg.NewStyle().Foreground(lg.Color(color)).Render(s) }
func strong(s, color string) string {
	return lg.NewStyle().Bold(true).Foreground(lg.Color(color)).Render(s)
}
func clip(s string, w int) string  { return ansi.Truncate(s, max(1, w), "…") }
func block(s string, w int) string { return ansi.Wrap(s, max(1, w), "") }
func statusColor(status string) string {
	switch {
	case strings.HasPrefix(status, "running"):
		return tuiMint
	case strings.HasPrefix(status, "missing"), strings.HasPrefix(status, "invalid"), strings.HasPrefix(status, "error"):
		return tuiRed
	default:
		return tuiAmber
	}
}

// Only the focused row has a background. Other cells inherit the terminal's
// background, including separators and padding; nested ANSI resets cannot leave
// a different-colored rectangle around a pane.
func rowText(s, color string, focused, bold bool) string {
	style := lg.NewStyle().Foreground(lg.Color(color)).Bold(bold)
	if focused {
		style = style.Background(lg.Color("#213744"))
	}
	return style.Render(s)
}
func row(s string, w int, focused bool) string {
	s = clip(s, w)
	padding := strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
	if focused {
		padding = rowText(padding, tuiWhite, true, false)
	}
	return s + padding
}
func fit(s string, w, h, offset int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	offset = min(offset, max(0, len(lines)-h))
	lines = lines[offset:min(len(lines), offset+h)]
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = row(line, w, false)
	}
	return strings.Join(lines, "\n")
}
func fieldLines(fields []Field, w int) string {
	labelWidth := 0
	for _, f := range fields {
		labelWidth = max(labelWidth, ansi.StringWidth(f.Label))
	}
	labelWidth = min(labelWidth, max(8, w/3))
	var lines []string
	for _, f := range fields {
		color := tuiWhite
		if f.Status {
			color = statusColor(f.Value)
		}
		if f.Warning {
			color = tuiRed
		}
		value := Safe(f.Value)
		if value == "" {
			value = "—"
		}
		// Safe neutralizes control characters; explicit value lines still retain
		// their structure without allowing terminal escapes from stored metadata.
		parts := strings.Split(f.Value, "\n")
		if f.Values != nil {
			parts = make([]string, len(f.Values))
			for i, entry := range f.Values {
				parts[i] = Safe(entry)
			}
			if len(parts) == 0 {
				parts = []string{"None"}
			}
		} else if len(parts) == 1 {
			parts = []string{value}
		} else {
			for i := range parts {
				parts[i] = Safe(parts[i])
			}
		}
		for i, part := range parts {
			label := ""
			if i == 0 {
				label = clip(Safe(f.Label), labelWidth)
			}
			prefix := tint(label+strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label))), tuiMuted) + "  "
			wrapped := strings.Split(block(tint(part, color), w-labelWidth-2), "\n")
			lines = append(lines, prefix+wrapped[0])
			for _, continuation := range wrapped[1:] {
				lines = append(lines, strings.Repeat(" ", labelWidth+2)+continuation)
			}
		}
	}
	return strings.Join(lines, "\n")
}
func heading(title string, w int, focused bool) string {
	color := tuiMuted
	if focused {
		color = tuiMint
	}
	return strong(clip(Safe(title), w), color) + "\n" + tint(strings.Repeat("─", w), tuiLine)
}
func (m *terminalModel) header(w int) string {
	title := ""
	if c := m.req.page.Collection; c != nil {
		title = c.Title
	} else if n := m.req.page.Navigation; n != nil {
		title = n.Collection.Title
	}
	brand := strong("Devbox Neo", tuiMint)
	if title != "" {
		tabs := []string{}
		for _, label := range []string{"Sessions", "Configs"} {
			if title == label {
				tabs = append(tabs, strong("[ "+label+" ]", tuiWhite))
			} else {
				tabs = append(tabs, tint("  "+label+"  ", tuiMuted))
			}
		}
		hint := ""
		if m.req.canTab {
			hint = tint("   Tab ↔", tuiMuted)
		}
		tabsLine := strings.Join(tabs, "  ") + hint
		if ansi.StringWidth(brand+"    "+tabsLine) <= w {
			return brand + "    " + tabsLine + "\n"
		}
		return brand + "\n" + tabsLine
	}
	return brand + "\n"
}
func (m *terminalModel) objectList(c Collection, key, query string, w, h int, active bool) string {
	indices := matchingItems(c, query)
	cursor := -1
	if active {
		indices = m.matches()
		cursor = min(m.cursor, max(0, len(indices)-1))
	} else {
		for pos, index := range indices {
			if c.Items[index].Key == key {
				cursor = pos
				break
			}
		}
	}
	capacity := max(1, h-3)
	stateWidth, activityWidth := 0, 0
	for _, item := range c.Items {
		stateWidth = max(stateWidth, ansi.StringWidth(Safe(item.Status)))
		if item.Activity != "" {
			activityWidth = max(activityWidth, len("Last active"), ansi.StringWidth(Safe(item.Activity)))
		}
	}
	if activityWidth > 0 {
		stateWidth = max(stateWidth, len("State"))
	}
	stackActivity := activityWidth > 0 && w-2-stateWidth-activityWidth-4 < 8
	var rows []string
	selectedEnd := 0
	for pos, index := range indices {
		item := c.Items[index]
		prefix := "  "
		if pos == cursor {
			if active {
				prefix = "▸ "
			} else {
				prefix = "› "
			}
		}
		label := strings.Repeat("  ", item.Depth) + Safe(item.Label)
		marker := ""
		if item.Selected {
			marker = " *"
		}
		color := tuiWhite
		if item.Folder {
			color = tuiMuted
		}
		if pos == cursor {
			color = tuiMint
		}
		focused := active && pos == cursor
		trailing := ""
		if !item.Folder {
			if activityWidth > 0 && !stackActivity {
				state := Safe(item.Status)
				trailing = rowText(state+strings.Repeat(" ", max(0, stateWidth-ansi.StringWidth(state))), statusColor(item.Status), focused, false)
				trailing += rowText("  "+Safe(item.Activity), tuiMuted, focused, false)
				trailing += rowText(strings.Repeat(" ", max(0, activityWidth-ansi.StringWidth(Safe(item.Activity)))), tuiMuted, focused, false)
			} else if item.Status != "" {
				trailing = rowText(Safe(item.Status), statusColor(item.Status), focused, false)
			}
		}
		text := rowText(prefix, tuiMint, focused, false) + rowText(clip(label, max(4, w-ansi.StringWidth(trailing)-len(marker)-3)), color, focused, true) + rowText(marker, tuiMint, focused, true)
		if trailing != "" {
			text += rowText(strings.Repeat(" ", max(1, w-ansi.StringWidth(text)-ansi.StringWidth(trailing))), tuiWhite, focused, false) + trailing
		}
		rows = append(rows, row(text, w, focused))
		if stackActivity && item.Activity != "" {
			rows = append(rows, row(rowText("    "+Safe(item.Activity), tuiMuted, focused, false), w, focused))
		}
		if pos == cursor {
			selectedEnd = len(rows)
		}
	}
	if len(indices) == 0 {
		rows = append(rows, tint("No matches", tuiMuted))
	}
	search := ""
	if m.searching && active {
		search = m.input.View()
	} else if m.query != "" && active {
		search = tint("/ "+Safe(m.query), tuiMuted)
	}
	title := heading(c.Title, w, active)
	if stackActivity {
		title = heading(c.Title+" · Last active", w, active)
	}
	if activityWidth > 0 && !stackActivity {
		labelWidth := w - stateWidth - activityWidth - 4
		color := tuiMuted
		if active {
			color = tuiMint
		}
		title = row(strong(clip(c.Title, labelWidth), color), labelWidth, false) + "  " + row(tint("State", tuiMuted), stateWidth, false) + "  " + row(tint("Last active", tuiMuted), activityWidth, false) + "\n" + tint(strings.Repeat("─", w), tuiLine)
	}
	return title + "\n" + fit(strings.Join(rows, "\n"), w, capacity, max(0, selectedEnd-capacity)) + "\n" + clip(search, w)
}
func (m *terminalModel) actionList(w, h int, active bool) string {
	if h <= 0 {
		return ""
	}
	indices := make([]int, len(m.req.page.Actions))
	for i := range indices {
		indices[i] = i
	}
	cursor := -1
	if active {
		indices = m.matches()
		cursor = min(m.cursor, max(0, len(indices)-1))
	}
	// Build visual rows separately from action indices so group separators never
	// affect dispatch or leave the keyboard cursor below the visible viewport.
	var rows []string
	selectedRow, labelWidth := 0, 0
	for _, index := range indices {
		a := m.req.page.Actions[index]
		if a.Value != "" {
			labelWidth = max(labelWidth, ansi.StringWidth(Safe(a.Label)))
		}
	}
	labelWidth = min(labelWidth, max(8, w/2))
	for pos, index := range indices {
		a := m.req.page.Actions[index]
		if a.BreakBefore && len(rows) > 0 {
			rows = append(rows, "")
		}
		if pos == cursor {
			selectedRow = len(rows)
		}
		prefix := "  "
		if pos == cursor {
			prefix = "▸ "
		}
		label := Safe(a.Label)
		if a.Selected {
			label = "* " + label
		}
		if a.Checked != nil {
			if *a.Checked {
				label = "✓ " + label
			} else {
				label = "○ " + label
			}
		}
		color := tuiWhite
		if a.Danger {
			color = tuiRed
		} else if pos == cursor {
			color = tuiMint
		}
		focused := active && pos == cursor
		if a.Value != "" {
			label = clip(label, labelWidth)
			label += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label)))
		}
		text := rowText(prefix, tuiMint, focused, false) + rowText(label, color, focused, true)
		if a.Value != "" {
			text += rowText("  "+Safe(a.Value), tuiMuted, focused, false)
		}
		if a.Shortcut != "" {
			text += rowText(" ["+a.Shortcut+"]", tuiMuted, focused, false)
		}
		if a.Status != "" {
			text += rowText("  "+Safe(a.Status), statusColor(a.Status), focused, false)
		}
		rows = append(rows, row(text, w, focused))
	}
	start := max(0, selectedRow-h+1)
	if len(rows) == 0 && m.query != "" {
		rows = append(rows, tint("No matches", tuiMuted))
	}
	return fit(strings.Join(rows, "\n"), w, h, start)
}
func (m *terminalModel) actionDetail(w, h int) string {
	indices := m.matches()
	if m.objects || len(indices) == 0 {
		return fit("", w, h, 0)
	}
	a := m.req.page.Actions[indices[min(m.cursor, len(indices)-1)]]
	detail := tint(Safe(a.Description), tuiMuted)
	if len(a.Fields) > 0 {
		detail = fieldLines(a.Fields, w)
	} else if a.Detail != "" {
		detail = block(a.Detail, w)
	}
	if a.Blocked != "" {
		detail = tint(Safe(a.Blocked), tuiAmber)
	}
	if m.searching {
		detail = m.input.View()
	} else if m.query != "" {
		detail = tint("/ "+Safe(m.query), tuiMuted) + "\n" + detail
	}
	return fit(block(detail, w), w, h, m.scroll)
}
func (m *terminalModel) browserContent(w, h int) string {
	c := m.req.page.Collection
	var preview string
	if item := m.currentObject(); item != nil {
		preview = strong(Safe(item.Label), tuiWhite) + "\n\n" + fieldLines(item.Fields, w)
	} else if len(c.Items) == 0 {
		preview = strong(c.Empty, tuiWhite)
	}
	if strings.Contains(m.req.notice, "\n") {
		preview = tint(m.req.notice, tuiAmber) + "\n\n" + preview
	}
	actionH := min(len(m.req.page.Actions), max(3, h/2))
	detailH := 2
	previewH := max(1, h-actionH-detailH-4)
	return fit(preview, w, previewH, m.scroll) + "\n" + heading("Application actions", w, !m.objects) + "\n" + m.actionList(w, actionH, !m.objects) + "\n" + m.actionDetail(w, detailH)
}
func (m *terminalModel) workflowContent(w, h int) string {
	title := m.req.page.Title
	context := fieldLines(m.req.page.Fields, w)
	if m.req.page.Prompt != "" {
		context += "\n" + tint(Safe(m.req.page.Prompt), tuiMuted)
	}
	if m.req.body != "" {
		context += "\n" + block(strings.TrimSpace(m.req.body), w)
	}
	if strings.Contains(m.req.notice, "\n") {
		context = tint(m.req.notice, tuiAmber) + "\n\n" + context
	}
	context = strings.TrimSpace(context)
	if m.req.input {
		return heading(title, w, true) + "\n" + fit(block(m.req.prompt, w)+"\n\n"+m.input.View()+"\n\n"+context, w, h-2, m.scroll)
	}
	if len(m.req.page.Actions) == 0 {
		return heading(title, w, true) + "\n" + fit(context, w, h-2, m.scroll)
	}
	contextH := 0
	if context != "" {
		contextH = min(lg.Height(context), max(2, (h-5)/3)) + 1
	}
	detailH := 2
	for _, a := range m.req.page.Actions {
		if len(a.Fields) > 0 || a.Detail != "" {
			detailH = min(5, max(2, h/5))
			break
		}
	}
	actionH := max(1, h-2-contextH-detailH-1)
	content := heading(title, w, true) + "\n"
	if contextH > 0 {
		content += fit(context, w, contextH-1, m.scroll) + "\n\n"
	}
	return content + m.actionList(w, actionH, true) + "\n\n" + m.actionDetail(w, detailH)
}
func (m *terminalModel) confirmation(w, h int) string {
	text := strings.TrimSpace(m.req.body + "\n\n" + m.req.prompt)
	body := heading("Confirm action", w, true) + "\n" + fit(block(text, w), w, h-5, m.scroll) + "\n\n"
	for i, label := range []string{"No — keep unchanged", "Yes — proceed"} {
		prefix := "  "
		if m.cursor == i {
			prefix = "▸ "
		}
		color := tuiWhite
		if i == 1 {
			color = tuiRed
		}
		body += row(rowText(prefix+label, color, m.cursor == i, true), w, m.cursor == i)
		if i == 0 {
			body += "\n"
		}
	}
	return body
}
func (m *terminalModel) View() tea.View {
	if m.width < 48 || m.height < 20 {
		v := tea.NewView("Devbox Neo\nEnlarge to at least 48 × 20.\nCtrl-C exits.")
		v.AltScreen = true
		return v
	}
	w := m.width - 4
	header := m.header(w)
	bodyH := m.height - lg.Height(header) - 5
	var body string
	if m.waiting {
		spin := []string{"◐", "◓", "◑", "◒"}[m.clock%4]
		body = fit(tint(spin+" Working…", tuiMuted), w, bodyH, 0)
	} else if m.req.confirm {
		body = m.confirmation(w, bodyH)
	} else {
		var c *Collection
		key, query := "", ""
		if m.req.page.Collection != nil {
			c = m.req.page.Collection
			key = m.objectKey
		} else if m.req.page.Navigation != nil {
			c = &m.req.page.Navigation.Collection
			key, query = m.req.page.Navigation.Key, m.req.page.Navigation.Query
		}
		content := m.workflowContent
		if m.req.page.Collection != nil {
			content = m.browserContent
		}
		if c != nil && len(c.Items) > 0 && w >= 84 {
			left := min(48, max(28, w*36/100))
			if c.Title == "Sessions" {
				left = min(56, max(38, w*40/100))
			}
			nav := m.objectList(*c, key, query, left, bodyH, m.objects)
			right := fit(content(w-left-3, bodyH), w-left-3, bodyH, 0)
			separator := strings.TrimSuffix(strings.Repeat(" │ \n", bodyH), "\n")
			body = lg.JoinHorizontal(lg.Top, nav, tint(separator, tuiLine), right)
		} else if m.objects && c != nil {
			body = m.objectList(*c, key, query, w, bodyH, true)
		} else {
			body = fit(content(w, bodyH), w, bodyH, 0)
		}
	}
	keys := "↑↓ move  Enter select  Esc " + strings.ToLower(m.req.page.Back) + "  Ctrl-C exit"
	if m.req.page.Collection != nil && len(m.req.page.Collection.Items) > 0 {
		if m.objects {
			keys = "↑↓ move  Enter open menu  → actions  Esc exit  Ctrl-C exit"
		} else {
			keys = "↑↓ move  Enter select  ← browse  Esc back  Ctrl-C exit"
		}
	}
	if w < 70 {
		keys = "↑↓ Enter  ←→ panes  Esc back  Ctrl-C exit"
	}
	if m.req.input {
		keys = "Enter accept  Esc cancel  Ctrl-C exit"
	}
	if m.searching {
		keys = "Type to filter  Enter done  Esc clear  Ctrl-C exit"
	}
	if m.waiting {
		keys = "Ctrl-C cancels"
	}
	if !m.req.input && !m.waiting && !m.searching && ansi.StringWidth(keys)+29 <= w {
		keys += "  / filter  PgUp/PgDn details"
	}
	notice := ""
	if m.req.notice != "" {
		notice = tint(Safe(strings.Split(m.req.notice, "\n")[0]), tuiAmber)
	}
	content := header + "\n" + fit(body, w, bodyH, 0) + "\n" + clip(notice, w) + "\n" + tint(strings.Repeat("─", w), tuiLine) + "\n" + clip(tint(keys, tuiMuted), w)
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = "  " + clip(lines[i], w)
	}
	content = fit(strings.Join(lines, "\n"), m.width, m.height, 0)
	if !m.color {
		content = ansi.Strip(content)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
