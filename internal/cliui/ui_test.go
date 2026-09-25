package cliui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestActionsUseDisplayedSnapshotAndHandlers(t *testing.T) {
	var out bytes.Buffer
	ui := New(context.Background(), strings.NewReader("bad\n1\n2\n"), &out)
	builds, called := 0, 0
	err := ui.Run(func() (Screen, error) {
		builds++
		return Screen{Title: "Actions", Back: "Back", Actions: []Action{
			{Label: "Hidden", Hidden: true, Run: func() (bool, error) { t.Fatal("hidden action ran"); return true, nil }},
			{Label: "Same label", Blocked: "Add a config first.", Run: func() (bool, error) { t.Fatal("blocked action ran"); return true, nil }},
			{Label: "Same label", Run: func() (bool, error) { called++; return true, nil }},
		}}, nil
	})
	if err != nil || called != 1 || builds != 2 {
		t.Fatal(builds, called, err, out.String())
	}
	if strings.Contains(out.String(), "Hidden") || !strings.Contains(out.String(), "Add a config first.") {
		t.Fatal(out.String())
	}
}

func TestChooseKeepsOriginalActionsAndHiddenIndexes(t *testing.T) {
	var out bytes.Buffer
	called := false
	page := Screen{Back: "Back", Actions: []Action{{Label: "Hidden", Hidden: true}, {Label: "Visible", Run: func() (bool, error) { called = true; return true, nil }}}}
	ui := New(context.Background(), strings.NewReader("1\n"), &out)
	index, err := ui.Choose(page)
	if err != nil || index != 1 || called {
		t.Fatal(index, called, err)
	}
	if _, err := page.Actions[1].Run(); err != nil || !called {
		t.Fatal("Choose mutated its caller's actions")
	}
}

type observedInput struct {
	lines  []string
	before func(int)
	next   int
}

func (r *observedInput) Read(p []byte) (int, error) {
	if r.next == len(r.lines) {
		return 0, io.EOF
	}
	if r.before != nil {
		r.before(r.next)
	}
	line := r.lines[r.next]
	r.next++
	return copy(p, line), nil
}
func TestChildViewWaitsForBackBeforeParentReturns(t *testing.T) {
	var out bytes.Buffer
	input := &observedInput{lines: []string{"1\n", "nonsense\n", "0\n", "0\n"}}
	ui := New(context.Background(), input, &out)
	input.before = func(step int) {
		text := out.String()
		if step == 1 || step == 2 {
			view := strings.LastIndex(text, "Details")
			if view < 0 || strings.Contains(text[view:], "Parent") || !strings.Contains(text[view:], "[0]  Back") {
				t.Fatalf("parent leaked into child frame: %s", text)
			}
		}
	}
	err := ui.Run(func() (Screen, error) {
		return Screen{Title: "Parent", Back: "Exit", Actions: []Action{{Label: "Inspect", Run: func() (bool, error) {
			return false, ui.View("Details", func(out io.Writer) error { _, err := fmt.Fprintln(out, "combined settings"); return err })
		}}}}, nil
	})
	if err != nil || strings.Count(out.String(), "Parent") != 2 || !strings.Contains(out.String(), "Choose 0 to back.") {
		t.Fatal(err, out.String())
	}
}
func TestNestedControlsShareBufferedInput(t *testing.T) {
	var out bytes.Buffer
	ui := New(context.Background(), strings.NewReader("1\nvalue with spaces\n0\n0\n"), &out)
	value := ""
	err := ui.Run(func() (Screen, error) {
		return Screen{Title: "Parent", Back: "Exit", Actions: []Action{{Label: "Edit", Run: func() (bool, error) {
			var err error
			value, _, err = ui.Text("Value: ", nil)
			if err != nil {
				return false, err
			}
			return false, ui.View("Review", nil)
		}}}}, nil
	})
	if err != nil || value != "value with spaces" {
		t.Fatal(value, err, out.String())
	}
}
func TestTextBackEOFAndValidation(t *testing.T) {
	for _, tc := range []struct {
		input, value string
		accepted     bool
		err          error
	}{
		{":back\n", "", false, nil}, {"partial", "", false, io.EOF}, {"bad\n good \n", " good ", true, nil},
	} {
		var out bytes.Buffer
		ui := New(context.Background(), strings.NewReader(tc.input), &out)
		value, accepted, err := ui.Text("Value: ", func(value string) error {
			if value == "bad" {
				return errors.New("not allowed")
			}
			return nil
		})
		if value != tc.value || accepted != tc.accepted || !errors.Is(err, tc.err) {
			t.Fatal(value, accepted, err)
		}
	}
}
func TestConfirmationRequiresSubmittedAffirmative(t *testing.T) {
	for _, tc := range []struct {
		input string
		yes   bool
	}{{"y\n", true}, {"YES\n", true}, {"\n", false}, {"n\n", false}, {"y", false}, {"yes", false}} {
		var out bytes.Buffer
		yes, err := New(context.Background(), strings.NewReader(tc.input), &out).Confirm("Delete? [y/N] ")
		if err != nil || yes != tc.yes {
			t.Fatal(tc, yes, err)
		}
	}
}
func TestFatalErrorsAndCancellationDoNotRetry(t *testing.T) {
	failure := errors.New("write failed")
	var out bytes.Buffer
	ui := New(context.Background(), strings.NewReader("1\n1\n"), &out)
	calls := 0
	err := ui.Run(func() (Screen, error) {
		return Screen{Back: "Back", Actions: []Action{{Label: "Save", Run: func() (bool, error) { calls++; return false, failure }}}}, nil
	})
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatal(calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ui = New(ctx, strings.NewReader("1\n"), &out)
	if err := ui.Run(func() (Screen, error) { t.Fatal("cancelled runner built a screen"); return Screen{}, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSelectionMarkersRemainReadableWithoutColor(t *testing.T) {
	var out bytes.Buffer
	checked, unchecked := true, false
	ui := New(context.Background(), strings.NewReader("0\n"), &out)
	_, err := ui.Choose(Screen{Back: "Back", Actions: []Action{{Label: "Current", Selected: true}, {Label: "Enabled", Checked: &checked}, {Label: "Disabled", Checked: &unchecked}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Current (selected)", "✓ Enabled", "  Disabled"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("redirected output contains ANSI")
	}
}

type failingOutput struct{ err error }

func (w failingOutput) Write([]byte) (int, error) { return 0, w.err }
func TestScreenOutputFailureIsReturned(t *testing.T) {
	failure := errors.New("broken output")
	ui := New(context.Background(), strings.NewReader("1\n"), failingOutput{failure})
	_, err := ui.Select("Screen", []string{"Action"}, "Back")
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
