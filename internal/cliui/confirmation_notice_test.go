package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPlainConfirmationPresentsQueuedNoticesBeforeApproval(t *testing.T) {
	var out bytes.Buffer
	r := New(context.Background(), strings.NewReader("n\n"), &out)
	r.Notice("Warning: first source")
	r.Notice("Warning: second source")
	yes, err := r.Confirm("Proceed? [y/N] ")
	if err != nil || yes {
		t.Fatal(yes, err)
	}
	text := out.String()
	first, second, prompt := strings.Index(text, "first source"), strings.Index(text, "second source"), strings.Index(text, "Proceed?")
	if first < 0 || second <= first || prompt <= second {
		t.Fatal("approval preceded warnings", text)
	}
	if err := r.Finish(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "first source") != 1 {
		t.Fatal("notice remained queued", out.String())
	}
}

func TestNativeConfirmationContainsAllQueuedNoticeText(t *testing.T) {
	req := &screenRequest{page: Screen{Back: "Cancel"}, confirm: true, prompt: "Proceed?", notice: "Warning: first source\nWarning: second source"}
	m := newTerminalModel(req, false)
	text := m.confirmation(100, 30)
	for _, message := range []string{"Warning: first source", "Warning: second source", "Proceed?", "No — keep unchanged", "Yes — proceed"} {
		if !strings.Contains(text, message) {
			t.Fatal("confirmation lost context", message, text)
		}
	}
}
