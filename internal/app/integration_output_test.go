//go:build integration

package app

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Docker and SSH commands can write concurrently. Keep progress logging and the
// assertion/failure capture together so verbose runs show output as it arrives.
type integrationOutput struct {
	sync.Mutex
	data bytes.Buffer
	logf func(string, ...any)
}

func (b *integrationOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	n, err := b.data.Write(p)
	if text := strings.TrimRight(string(p), "\r\n"); text != "" {
		b.logf("%s", text)
	}
	return n, err
}

func (b *integrationOutput) String() string {
	b.Lock()
	defer b.Unlock()
	return b.data.String()
}

func (b *integrationOutput) Reset() {
	b.Lock()
	defer b.Unlock()
	b.data.Reset()
}

func TestIntegrationOutputCaptureAndProgress(t *testing.T) {
	var logs []string
	output := &integrationOutput{logf: func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}
	for _, text := range []string{"build step\r\n", "\n", "split", "chunk\n"} {
		if n, err := output.Write([]byte(text)); n != len(text) || err != nil {
			t.Fatalf("write: %d, %v", n, err)
		}
	}
	if got := output.String(); got != "build step\r\n\nsplitchunk\n" {
		t.Fatalf("capture changed command output: %q", got)
	}
	if got := strings.Join(logs, "|"); got != "build step|split|chunk" {
		t.Fatalf("progress: %q", got)
	}
	output.Reset()
	if got := output.String(); got != "" {
		t.Fatalf("reset retained output: %q", got)
	}
	if len(logs) != 3 {
		t.Fatal("reset changed previously emitted progress")
	}
}

func TestIntegrationOutputConcurrentWrites(t *testing.T) {
	var logs []string
	output := &integrationOutput{logf: func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}
	var writers sync.WaitGroup
	for worker := range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for line := range 20 {
				text := fmt.Sprintf("%d:%d\n", worker, line)
				if n, err := output.Write([]byte(text)); n != len(text) || err != nil {
					t.Errorf("write: %d, %v", n, err)
				}
				_ = output.String()
			}
		}()
	}
	writers.Wait()
	captured := strings.TrimSuffix(output.String(), "\n")
	if got := strings.Join(logs, "\n"); got != captured || len(logs) != 160 {
		t.Fatal("concurrent capture and progress differ or lost output")
	}
	for worker := range 8 {
		for line := range 20 {
			if text := fmt.Sprintf("%d:%d\n", worker, line); strings.Count(output.String(), text) != 1 {
				t.Fatalf("missing or duplicated output %q", text)
			}
		}
	}
}
