package cliui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Only the workflow goroutine accesses this adapter. Bubble Tea has one input
// reader and receives immutable screen snapshots; it never runs domain handlers.
// The one UI goroutine is joined at Finish, including cancellation and failures.
type terminalScreen struct {
	terminal, input *os.File
	ctx             context.Context
	cancel          context.CancelFunc
	pending         bytes.Buffer
	program         *tea.Program
	done            chan struct{}
	err             error
	resume          chan struct{}
	paused, closed  bool
}

func (s *terminalScreen) Write(p []byte) (int, error) {
	if s.paused || s.closed {
		return s.terminal.Write(p)
	}
	return s.pending.Write(p)
}
func Terminal(out io.Writer) *os.File {
	if s, ok := out.(*terminalScreen); ok {
		return s.terminal
	}
	file, _ := out.(*os.File)
	return file
}
func (s *terminalScreen) take() string { text := s.pending.String(); s.pending.Reset(); return text }
func (s *terminalScreen) start(req *screenRequest, working bool) {
	s.done = make(chan struct{})
	model := newTerminalModel(req, Colors(s.terminal).Enabled())
	model.cancel = s.cancel
	model.waiting = working
	// Command cancellation must still join the terminal reader. Bubble Tea's
	// external-context cancellation is a force exit, which skips that join.
	// Quit gracefully instead; handoff/workflow code keeps the original context.
	s.program = tea.NewProgram(model, tea.WithInput(s.input), tea.WithOutput(s.terminal), tea.WithContext(context.WithoutCancel(s.ctx)), tea.WithoutSignalHandler())
	quitDone := make(chan struct{})
	stopQuit := context.AfterFunc(s.ctx, func() {
		defer close(quitDone)
		s.program.Quit()
	})
	go func() {
		final, err := s.program.Run()
		s.err = err
		if err == nil {
			if state, ok := final.(*terminalModel); ok {
				s.err = state.err
			}
		}
		if s.err == nil {
			s.err = s.ctx.Err()
		}
		if !stopQuit() {
			<-quitDone
		}
		close(s.done)
	}()
}
func (s *terminalScreen) working() {
	if s.closed {
		return
	}
	if s.resume != nil {
		close(s.resume)
		s.resume = nil
	}
	s.paused = false
	if s.program == nil {
		s.start(&screenRequest{page: Screen{Title: "Devbox", Back: "Exit"}}, true)
	} else {
		s.program.Send(workingMsg{})
	}
}
func (s *terminalScreen) present(req *screenRequest) (screenReply, error) {
	if s.closed {
		return screenReply{}, io.EOF
	}
	if err := s.ctx.Err(); err != nil {
		return screenReply{}, err
	}
	if s.resume != nil {
		close(s.resume)
		s.resume = nil
	}
	s.paused = false
	req.reply = make(chan screenReply, 1)
	if s.program == nil {
		s.start(req, false)
	} else {
		s.program.Send(req)
	}
	select {
	case reply := <-req.reply:
		return reply, reply.err
	case <-s.done:
		select {
		case reply := <-req.reply:
			return reply, reply.err
		default:
		}
		if s.err != nil {
			return screenReply{}, s.err
		}
		return screenReply{}, io.EOF
	case <-s.ctx.Done():
		return screenReply{}, s.ctx.Err()
	}
}

// Exec suspends Bubble Tea's input and renderer together. The workflow continues
// synchronously after ready, and resumes the UI only at its next interaction.
// No second stdin reader competes with the harness, shell, or confirmation I/O.
type terminalHandoff struct {
	ctx    context.Context
	ready  chan error
	resume chan struct{}
}

func (h *terminalHandoff) Run() error {
	h.ready <- nil
	select {
	case <-h.resume:
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}
func (*terminalHandoff) SetStdin(io.Reader)  {}
func (*terminalHandoff) SetStdout(io.Writer) {}
func (*terminalHandoff) SetStderr(io.Writer) {}

type handoffEnded struct{ err error }

func (s *terminalScreen) leave() error {
	if s.ctx.Err() != nil && s.program != nil {
		// Cancellation can finish an interaction before the renderer has shut
		// down. Wait for restoration before domain code prints partial results.
		<-s.done
		s.paused = true
		_, err := s.pending.WriteTo(s.terminal)
		return err
	}
	if s.closed || s.paused {
		return nil
	}
	if s.program != nil {
		h := &terminalHandoff{ctx: s.ctx, ready: make(chan error, 1), resume: make(chan struct{})}
		s.resume = h.resume
		s.program.Send(h)
		select {
		case err := <-h.ready:
			if err != nil {
				return err
			}
		case <-s.done:
			if s.err != nil {
				return s.err
			}
			return io.EOF
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	s.paused = true
	_, err := s.pending.WriteTo(s.terminal)
	return err
}
func (s *terminalScreen) finish() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.resume != nil {
		close(s.resume)
		s.resume = nil
	}
	if s.program != nil {
		s.program.Quit()
		<-s.done
	}
	_, err := s.pending.WriteTo(s.terminal)
	terminalErr := s.err
	// Cancellation belongs to the interaction/command, not a second cleanup
	// failure. Preserve real terminal errors while avoiding duplicate reports.
	if terminalErr == context.Canceled || terminalErr == context.DeadlineExceeded {
		terminalErr = nil
	}
	return errors.Join(terminalErr, err)
}
func (r *Runner) terminalChoice(page Screen, actions []Action, cursor int, itemKey, query string) (screenReply, error) {
	// Body callbacks sometimes use the runner's writer themselves. Capture through
	// that same writer, not a second buffer that would lose their context.
	prefix := r.screen.take()
	if page.Body != nil {
		if err := page.Body(r.Out); err != nil {
			return screenReply{}, err
		}
	}
	body := prefix + r.screen.take()
	notices := strings.Join(r.notices, "\n")
	r.notices = nil
	canTab := page.OnTab != nil
	page.Actions = slices.Clone(actions)
	page.Fields = snapshotFields(page.Fields)
	page.Body = nil
	page.Rows = nil
	page.OnTab = nil
	for i := range page.Actions {
		a := &page.Actions[i]
		a.Run = nil
		a.Fields = snapshotFields(a.Fields)
		if a.Checked != nil {
			value := *a.Checked
			a.Checked = &value
		}
	}
	page.Collection = snapshotCollection(page.Collection)
	page.Navigation = snapshotNavigation(page.Navigation)
	return r.screen.present(&screenRequest{page: page, body: body, notice: notices, cursor: cursor, itemKey: itemKey, query: query, canTab: canTab})
}
func snapshotFields(fields []Field) []Field {
	fields = slices.Clone(fields)
	for i := range fields {
		fields[i].Values = slices.Clone(fields[i].Values)
	}
	return fields
}

func snapshotNavigation(n *Navigation) *Navigation {
	if n == nil {
		return nil
	}
	return &Navigation{Collection: *snapshotCollection(&n.Collection), Key: n.Key, Query: n.Query}
}

func snapshotCollection(c *Collection) *Collection {
	if c == nil {
		return nil
	}
	snapshot := *c
	snapshot.Items = slices.Clone(c.Items)
	for i := range snapshot.Items {
		snapshot.Items[i].Open = nil
		snapshot.Items[i].Fields = snapshotFields(snapshot.Items[i].Fields)
	}
	return &snapshot
}

func (r *Runner) terminalText(request TextRequest) (string, bool, error) {
	context := r.screen.take()
	notice := strings.Join(r.notices, "\n")
	r.notices = nil
	reply, err := r.screen.present(&screenRequest{page: Screen{Title: "Enter a value", Back: "Back", Navigation: snapshotNavigation(r.navigation)}, body: ansi.Strip(context), notice: notice, input: true, initial: request.Initial, sensitive: request.Sensitive, prompt: strings.ReplaceAll(request.Prompt, ":back", "Esc")})
	return reply.value, !reply.back && err == nil, err
}
func (r *Runner) terminalConfirm(prompt string) (bool, error) {
	reply, err := r.screen.present(&screenRequest{page: Screen{Title: "Confirm action", Back: "Cancel", Actions: []Action{{Label: "No — keep unchanged"}, {Label: "Yes — proceed", Danger: true}}}, body: ansi.Strip(r.screen.take()), prompt: prompt, confirm: true})
	if err != nil {
		pauseErr := r.Pause()
		if pauseErr == nil {
			_, pauseErr = fmt.Fprintf(r.Out, "%sCancelled.\n", prompt)
		}
		return false, errors.Join(err, pauseErr)
	}
	yes := !reply.back && reply.index == 1
	if err := r.Pause(); err != nil {
		return false, err
	}
	answer := "no"
	if yes {
		answer = "yes"
	}
	_, err = fmt.Fprintf(r.Out, "%s%s\n", prompt, answer)
	return yes, err
}
