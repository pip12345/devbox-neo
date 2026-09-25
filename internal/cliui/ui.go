// Package cliui provides synchronous, line-oriented screens. Workflows own
// their state and persistence; the runner owns interaction and terminal life.
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
	Hidden      bool
	Blocked     string
	Selected    bool
	Checked     *bool // Non-nil gives a toggle its explicit checked/unchecked marker.
	BreakBefore bool
	Run         func() (done bool, err error)
}

// Rows may supply a table layout for the actions. The filtered slice is the
// exact displayed snapshot used for dispatch; labels never select behavior.
type Screen struct {
	Title   string
	Body    func(io.Writer) error
	Prompt  string
	Actions []Action
	Rows    func(io.Writer, []Action) error
	Back    string
}

type Runner struct {
	Context context.Context
	Input   *bufio.Reader
	Out     io.Writer
	screen  *terminalScreen
	notices []string
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
		r.screen = &terminalScreen{terminal: file}
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
	if r.screen != nil {
		return r.screen.finish()
	}
	return nil
}
func (r *Runner) Pause() error {
	if r.screen != nil {
		return r.screen.leave()
	}
	return nil
}
func (r *Runner) PlainNext() {
	if r.screen != nil {
		r.screen.showNextPlain = true
	}
}

// Notice belongs to the next interaction, not to a partially rendered parent
// screen. A child can return feedback without drawing its parent underneath it.
func (r *Runner) Notice(text string) { r.notices = append(r.notices, text) }

func (r *Runner) Run(build func() (Screen, error)) error {
	for {
		if err := r.Context.Err(); err != nil {
			return err
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
		selected, err := r.readChoice(len(actions), page.Back)
		if err != nil || selected < 0 {
			return err
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
		if err != nil || done {
			return err
		}
	}
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
	var frame []byte
	if r.screen != nil {
		frame = r.screen.choiceFrame()
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
		if r.screen != nil {
			r.screen.retryChoice(frame, hint)
		} else if _, err := fmt.Fprint(r.Out, hint); err != nil {
			return -1, err
		}
	}
}

func (r *Runner) Line(prompt string) (string, error) {
	if err := r.Context.Err(); err != nil {
		return "", err
	}
	if r.screen != nil {
		if err := r.screen.show(prompt); err != nil {
			return "", err
		}
	} else if _, err := fmt.Fprint(r.Out, prompt); err != nil {
		return "", err
	}
	line, err := r.Input.ReadString('\n')
	if r.screen != nil {
		if displayErr := r.screen.afterInput(); displayErr != nil {
			return "", displayErr
		}
	}
	// EOF never submits partially typed input, including destructive approvals.
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func (r *Runner) Text(prompt string, validate func(string) error) (string, bool, error) {
	for {
		value, err := r.Line(prompt)
		if err != nil || value == ":back" {
			return "", false, err
		}
		if validate != nil {
			if err := validate(value); err != nil {
				if _, writeErr := fmt.Fprintf(r.Out, "Error: %s\n", Safe(err.Error())); writeErr != nil {
					return "", false, writeErr
				}
				continue
			}
		}
		return value, true, nil
	}
}

func (r *Runner) Confirm(prompt string) (bool, error) {
	// Approvals and their context belong in the shell transcript, including
	// when invoked from a redrawable menu. Never hide a destructive decision.
	if err := r.Pause(); err != nil {
		return false, err
	}
	out := r.Out
	if r.screen != nil {
		if _, err := r.screen.pending.WriteTo(r.screen.terminal); err != nil {
			return false, err
		}
		out = r.screen.terminal
	}
	if err := r.Context.Err(); err != nil {
		return false, err
	}
	if _, err := fmt.Fprint(out, prompt); err != nil {
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
