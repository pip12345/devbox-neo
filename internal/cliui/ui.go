// Package cliui exposes synchronous workflows over a command-scoped terminal UI.
// Bubble Tea owns interactive input/rendering; callers own state and persistence.
package cliui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

type Action struct {
	Label       string
	Description string
	Value       string
	Fields      []Field
	Status      string
	Detail      string
	Shortcut    string
	Danger      bool
	Hidden      bool
	Blocked     string
	Selected    bool
	Checked     *bool // Non-nil gives a toggle its explicit checked/unchecked marker.
	BreakBefore bool
	Run         func() (done bool, err error)
}

// Items are browsed objects, not commands. Stable keys retain selection across
// inventory refreshes. Open runs only after an explicit selection.
type Item struct {
	Key, Label, Description, Status string
	Activity                        string
	Depth                           int
	Selected, Folder                bool
	Fields                          []Field
	Open                            func() error
}

type Field struct {
	Label, Value string
	Values       []string
	Status       bool
	Warning      bool
}

type Collection struct {
	Title, Empty string
	Items        []Item
}

// Navigation keeps the object list visible while a nested workflow owns input.
// A workflow can replace it with a refreshed snapshot after an operation.
type Navigation struct {
	Collection Collection
	Key, Query string
}

// Rows may supply a table layout for the actions. The filtered slice is the
// exact displayed snapshot used for dispatch; labels never select behavior.
type Screen struct {
	Title      string
	Body       func(io.Writer) error
	Prompt     string
	Actions    []Action
	Rows       func(io.Writer, []Action) error
	Back       string
	OnTab      func() (done bool, err error)
	Fields     []Field
	Collection *Collection
	Navigation *Navigation
	FocusItem  string
}

type Runner struct {
	Context    context.Context
	Input      *bufio.Reader
	Out        io.Writer
	screen     *terminalScreen
	notices    []string
	receipts   []string
	navigation *Navigation
}

func New(ctx context.Context, in io.Reader, out io.Writer) *Runner {
	if ctx == nil {
		ctx = context.Background()
	}
	r := &Runner{Context: ctx, Out: out}
	inputFile, inputTTY := in.(*os.File)
	if inputTTY && IsTerminal(inputFile) {
		in = terminalReader{ctx: ctx, file: inputFile}
	}
	if reader, ok := in.(*bufio.Reader); ok {
		r.Input = reader
	} else {
		r.Input = bufio.NewReader(in)
	}
	if file := Terminal(out); inputTTY && IsTerminal(inputFile) && file != nil && IsTerminal(file) && os.Getenv("TERM") != "dumb" {
		uiContext, cancel := context.WithCancel(ctx)
		r.Context = uiContext
		r.screen = &terminalScreen{terminal: file, input: inputFile, ctx: uiContext, cancel: cancel}
		r.Out = r.screen
	}
	return r
}

func (r *Runner) Redraws() bool { return r.screen != nil }
func (r *Runner) Finish() error {
	for _, notice := range r.notices {
		if _, err := fmt.Fprintln(r.Out, notice); err != nil {
			return err
		}
	}
	r.notices = nil
	var err error
	if r.screen != nil {
		err = r.screen.finish()
	}
	for _, receipt := range r.receipts {
		if _, writeErr := fmt.Fprintln(r.Out, receipt); writeErr != nil && err == nil {
			err = writeErr
		}
	}
	r.receipts = nil
	return err
}
func (r *Runner) Receipt(text string) { r.receipts = append(r.receipts, text) }
func (r *Runner) Pause() error {
	if r.screen != nil {
		return r.screen.leave()
	}
	return nil
}

// ReviewOutput keeps streamed warnings/results visible before restoring menus.
func (r *Runner) ReviewOutput() error {
	if r.screen == nil {
		return nil
	}
	if err := r.Pause(); err != nil {
		return err
	}
	if _, err := fmt.Fprint(r.Out, "\nPress Enter to return to the menu… "); err != nil {
		return err
	}
	_, err := r.Input.ReadString('\n')
	return err
}

// Notice belongs to the next interaction, not to a partially rendered parent
// screen. A child can return feedback without drawing its parent underneath it.
func (r *Runner) Notice(text string) { r.notices = append(r.notices, text) }

// RefreshNavigation updates the shared workflow frame, including parents that
// resume after a child returns. Presented snapshots are deep copies, so the UI
// goroutine never observes a collection changing underneath it.
func (r *Runner) RefreshNavigation(collection Collection) {
	if r.navigation != nil && r.navigation.Collection.Title == collection.Title {
		r.navigation.Collection = collection
	}
}

func (r *Runner) Run(build func() (Screen, error)) error {
	cursor, query := -1, ""
	itemKey := ""
	parentNavigation := r.navigation
	defer func() { r.navigation = parentNavigation }()
	for {
		if err := r.Context.Err(); err != nil {
			return err
		}
		if r.screen != nil {
			r.screen.working()
		}
		page, err := build()
		if err != nil {
			return err
		}
		actions := make([]Action, 0, len(page.Actions))
		for _, action := range page.Actions {
			if !action.Hidden {
				actions = append(actions, action)
			}
		}
		if page.Navigation != nil {
			if r.navigation != nil && r.navigation.Collection.Title == page.Navigation.Collection.Title {
				page.Navigation.Query = r.navigation.Query
			}
			r.navigation = page.Navigation
		} else {
			page.Navigation = r.navigation
		}
		selected, tab, item := -1, false, false
		if r.screen != nil {
			var reply screenReply
			reply, err = r.terminalChoice(page, actions, cursor, itemKey, query)
			selected, tab, item, query = reply.index, reply.tab, reply.item, reply.query
			if reply.focusItem != "" {
				itemKey = reply.focusItem
			}
		} else {
			plain := actions
			if page.Collection != nil {
				plain = make([]Action, 0, len(page.Collection.Items)+len(actions))
				for _, object := range page.Collection.Items {
					plain = append(plain, Action{Label: object.Label, Description: object.Description, Selected: object.Selected})
				}
				plain = append(plain, actions...)
			}
			err = r.renderPlainPage(page, plain)
			if err == nil {
				selected, err = r.readChoice(len(plain), page.Back)
				if selected >= 0 && page.Collection != nil {
					item = selected < len(page.Collection.Items)
					if !item {
						selected -= len(page.Collection.Items)
					}
				}
			}
		}
		if err != nil {
			return err
		}
		if err := r.Context.Err(); err != nil {
			return err
		}
		if tab {
			cursor, query, itemKey = -1, "", ""
			done, err := page.OnTab()
			if err == nil {
				err = r.Context.Err()
			}
			if err != nil || done {
				return err
			}
			continue
		}
		if selected < 0 {
			return r.Context.Err()
		}
		if item {
			object := page.Collection.Items[selected]
			itemKey, cursor = object.Key, -1
			r.navigation = &Navigation{Collection: *page.Collection, Key: object.Key, Query: query}
			if object.Open == nil {
				return fmt.Errorf("object %q has no opener", object.Label)
			}
			if err := object.Open(); err != nil {
				return err
			}
			r.navigation = parentNavigation
			continue
		}
		cursor = selected
		if page.Collection != nil {
			r.navigation = &Navigation{Collection: *page.Collection}
		}
		action := actions[selected]
		if action.Blocked != "" {
			r.Notice(action.Blocked)
			continue
		}
		if action.Run == nil {
			return fmt.Errorf("menu action %q has no handler", action.Label)
		}
		done, err := action.Run()
		if err == nil {
			err = r.Context.Err()
		}
		if err != nil || done {
			return err
		}
	}
}

func (r *Runner) renderPlainPage(page Screen, actions []Action) error {
	for _, notice := range r.notices {
		if _, err := fmt.Fprintln(r.Out, notice); err != nil {
			return err
		}
	}
	r.notices = nil
	if page.Title != "" {
		if err := Title(r.Out, page.Title); err != nil {
			return err
		}
	}
	if page.Body != nil {
		if err := page.Body(r.Out); err != nil {
			return err
		}
	}
	for _, field := range page.Fields {
		if field.Values != nil {
			if _, err := fmt.Fprintf(r.Out, "%s:\n", Safe(field.Label)); err != nil {
				return err
			}
			for _, value := range field.Values {
				if _, err := fmt.Fprintln(r.Out, "  "+Safe(value)); err != nil {
					return err
				}
			}
		} else if _, err := fmt.Fprintf(r.Out, "%s: %s\n", Safe(field.Label), Safe(field.Value)); err != nil {
			return err
		}
	}
	if page.Prompt != "" {
		if err := Title(r.Out, page.Prompt); err != nil {
			return err
		}
	}
	if page.Rows != nil {
		if err := page.Rows(r.Out, actions); err != nil {
			return err
		}
	} else {
		for i, action := range actions {
			if action.BreakBefore {
				if _, err := fmt.Fprintln(r.Out); err != nil {
					return err
				}
			}
			label := Safe(action.Label)
			if action.Value != "" {
				label += ": " + Safe(action.Value)
			}
			var style func(string) string
			if action.Checked != nil {
				if *action.Checked {
					label = "✓ " + label
					paint := Colors(r.Out)
					style = func(s string) string { return strings.ReplaceAll(paint.Strong(s), "✓", paint.Green("✓")) }
				} else {
					label = "  " + label
				}
			} else if action.Selected {
				label += " (selected)"
				paint := Colors(r.Out)
				style = func(s string) string {
					return strings.ReplaceAll(paint.Strong(s), "(selected)", paint.Green("(selected)"))
				}
			}
			prefix := Prefix(i + 1)
			if err := WriteLine(r.Out, prefix, label, strings.Repeat(" ", len(prefix)), Width(r.Out), style); err != nil {
				return err
			}
		}
	}
	return nil
}

// Choose and View use the same dispatcher as action menus. A view with no
// actions is still an interaction: its parent resumes only after Back.
func (r *Runner) Choose(page Screen) (int, error) {
	page.Actions = slices.Clone(page.Actions)
	selected := -1
	for i := range page.Actions {
		index := i
		page.Actions[i].Run = func() (bool, error) { selected = index; return true, nil }
	}
	err := r.Run(func() (Screen, error) { return page, nil })
	return selected, err
}
func (r *Runner) Select(title string, labels []string, back string) (int, error) {
	actions := make([]Action, len(labels))
	for i, label := range labels {
		actions[i].Label = label
	}
	return r.Choose(Screen{Title: title, Actions: actions, Back: back})
}

func (r *Runner) SelectCurrent(title string, labels []string, selected int, summary, back string) (int, error) {
	if summary == "" {
		summary = "None"
		if selected >= 0 && selected < len(labels) {
			summary = labels[selected]
		}
	}
	actions := make([]Action, len(labels))
	for i, label := range labels {
		actions[i] = Action{Label: Safe(label), Selected: i == selected}
	}
	return r.Choose(Screen{Title: title, Back: back, Actions: actions, Body: func(out io.Writer) error {
		if err := WriteLine(out, "Current selection: ", Safe(summary), "  ", Width(out), Colors(out).Strong); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out)
		return err
	}})
}

func (r *Runner) View(title string, body func(io.Writer) error) error {
	return r.Run(func() (Screen, error) { return Screen{Title: title, Body: body, Back: "Back"}, nil })
}

const ChoicePrompt = "\n   Choose a number > "

func Prefix(n int) string { return fmt.Sprintf("   %-4s ", fmt.Sprintf("[%d]", n)) }

func (r *Runner) readChoice(count int, back string) (int, error) {
	if _, err := fmt.Fprintf(r.Out, "\n%s%s\n", Prefix(0), back); err != nil {
		return -1, err
	}
	for {
		line, err := r.Line(ChoicePrompt)
		if err != nil {
			return -1, err
		}
		line = strings.TrimSpace(line)
		if line == "0" || line == "q" {
			return -1, nil
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= count {
			return n - 1, nil
		}
		hint := fmt.Sprintf("Choose 1–%d, or 0 to %s.\n", count, strings.ToLower(back))
		if count == 0 {
			hint = fmt.Sprintf("Choose 0 to %s.\n", strings.ToLower(back))
		}
		if _, err := fmt.Fprint(r.Out, hint); err != nil {
			return -1, err
		}
	}
}

func (r *Runner) Line(prompt string) (string, error) {
	if err := r.Context.Err(); err != nil {
		return "", err
	}
	if r.screen != nil {
		value, accepted, err := r.terminalText(prompt)
		if err == nil && !accepted {
			return "", io.EOF
		}
		return value, err
	}
	if _, err := fmt.Fprint(r.Out, prompt); err != nil {
		return "", err
	}
	line, err := r.Input.ReadString('\n')
	// EOF never submits partially typed input, including destructive approvals.
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func (r *Runner) Text(prompt string, validate func(string) error) (string, bool, error) {
	for {
		var value string
		var err error
		accepted := true
		if r.screen != nil {
			value, accepted, err = r.terminalText(prompt)
		} else {
			value, err = r.Line(prompt)
			accepted = value != ":back"
		}
		if err != nil || !accepted {
			return "", false, err
		}
		if validate != nil {
			if err := validate(value); err != nil {
				if r.screen != nil {
					r.Notice("Error: " + Safe(err.Error()))
				} else if _, writeErr := fmt.Fprintf(r.Out, "Error: %s\n", Safe(err.Error())); writeErr != nil {
					return "", false, writeErr
				}
				continue
			}
		}
		return value, true, nil
	}
}

func (r *Runner) Confirm(prompt string) (bool, error) {
	if r.screen != nil {
		return r.terminalConfirm(prompt)
	}
	if err := r.Context.Err(); err != nil {
		return false, err
	}
	if _, err := fmt.Fprint(r.Out, prompt); err != nil {
		return false, err
	}
	value, err := r.Input.ReadString('\n')
	if err == io.EOF {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes", nil
}
