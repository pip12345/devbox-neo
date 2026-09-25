package cliui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTextValidationRetainsPendingValueInPlainContext(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("bad\ngood\n"), &out)
	value, accepted, err := r.Text(TextRequest{Prompt: "Value: ", Initial: "original", Validate: func(value string) error {
		if value == "bad" {
			return errors.New("try again")
		}
		return nil
	}})
	if err != nil || !accepted || value != "good" || !strings.Contains(out.String(), "Current value: original") || !strings.Contains(out.String(), "Current value: bad") {
		t.Fatal(value, accepted, err, out.String())
	}
	out.Reset()
	r = New(context.Background(), strings.NewReader("\n"), &out)
	value, accepted, err = r.Text(TextRequest{Prompt: "Value: ", Initial: "original"})
	if err != nil || !accepted || value != "" {
		t.Fatal("blank acted as keep", value, accepted, err)
	}
}

func TestNativeTextPrefillsAndMasksWithoutChangingSubmission(t *testing.T) {
	for _, sensitive := range []bool{false, true} {
		req := request()
		req.input, req.sensitive, req.initial = true, sensitive, "private-value"
		m := newTerminalModel(req, false)
		if m.input.Value() != req.initial || m.input.Position() != len(req.initial) {
			t.Fatal("initial text was not editable", m.input.Value())
		}
		view := ansi.Strip(m.View().Content)
		if strings.Contains(view, req.initial) == sensitive {
			t.Fatal("wrong echo mode", sensitive, view)
		}
		key(m, tea.KeyEnter, "")
		if reply := <-req.reply; reply.value != req.initial {
			t.Fatal("mask changed stored value", reply)
		}
	}
}

func TestTextControlCharactersUseExplicitJSONAndSensitiveFailuresStayPrivate(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("\"line\\nnext\\tlast\"\n"), &out)
	value, accepted, err := r.Text(TextRequest{Prompt: "Argument: ", Initial: "line\nnext\tlast"})
	if err != nil || !accepted || value != "line\nnext\tlast" || !strings.Contains(out.String(), "JSON string") {
		t.Fatal(value, accepted, err, out.String())
	}
	out.Reset()
	r = New(context.Background(), strings.NewReader("new-secret\n:back\n"), &out)
	_, accepted, err = r.Text(TextRequest{Prompt: "Secret: ", Initial: "old-secret", Sensitive: true, Validate: func(value string) error { return errors.New(value) }})
	if err != nil || accepted || strings.Contains(out.String(), "old-secret") || strings.Contains(out.String(), "new-secret") {
		t.Fatal("sensitive value leaked", out.String(), err)
	}
}
