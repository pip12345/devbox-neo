package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"
)

type terminalProbe struct {
	t             *testing.T
	master, slave *os.File
	mu            sync.Mutex
	text          strings.Builder
	done          chan struct{}
	mark          int
}

func newTerminalProbe(t *testing.T) *terminalProbe {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 34, Col: 112}); err != nil {
		t.Fatal(err)
	}
	p := &terminalProbe{t: t, master: master, slave: slave, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.text.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		master.Close()
		slave.Close()
		select {
		case <-p.done:
		case <-time.After(time.Second):
			t.Error("terminal reader did not stop")
		}
	})
	return p
}
func (p *terminalProbe) output() string { p.mu.Lock(); defer p.mu.Unlock(); return p.text.String() }
func (p *terminalProbe) wait(text string) {
	p.t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		all := p.output()
		if strings.Contains(ansi.Strip(all[p.mark:]), text) {
			p.mark = len(all)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatalf("terminal did not show %q: %q", text, p.output())
}
func (p *terminalProbe) send(keys string) {
	p.t.Helper()
	if _, err := io.WriteString(p.master, keys); err != nil {
		p.t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
}
func (p *terminalProbe) workflow(run func(context.Context, *os.File) error) chan error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	p.t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- run(ctx, p.slave) }()
	return done
}
func (p *terminalProbe) drain() {
	p.t.Helper()
	if _, err := p.slave.Write([]byte{0}); err != nil {
		p.t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.output(), "\x00") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	p.t.Fatal("terminal output did not drain")
}
func (p *terminalProbe) finish(done chan error) {
	p.t.Helper()
	select {
	case err := <-done:
		if err != nil {
			p.t.Fatal(err, p.output())
		}
	case <-time.After(4 * time.Second):
		p.t.Fatal("terminal workflow did not finish", p.output())
	}
	p.drain()
}
