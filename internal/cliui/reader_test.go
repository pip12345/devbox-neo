package cliui

import (
	"context"
	"io"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

type heldInput struct{ entered, release chan struct{} }

func (r heldInput) Read([]byte) (int, error) {
	close(r.entered)
	<-r.release
	return 0, io.EOF
}

// Bubble Tea closes its cancel reader after StreamEvents returns. Returning
// while the internal read goroutine is still active races with descriptor
// closure and can leave a reader behind at terminal handoff.
func TestTerminalReaderJoinsItsInputGoroutine(t *testing.T) {
	input := heldInput{make(chan struct{}), make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := uv.NewTerminalReader(input, "xterm-256color")
	done := make(chan error, 1)
	go func() { done <- reader.StreamEvents(ctx, make(chan uv.Event, 1)) }()
	<-input.entered
	cancel()
	select {
	case err := <-done:
		close(input.release)
		t.Fatalf("reader returned before its input goroutine exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(input.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not join after input was released")
	}
}
