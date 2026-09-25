package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

// A subprocess owns the PTY's foreground process group so Ctrl-C exercises the
// real SIGINT route, not just an injected context cancellation.
func TestForegroundSIGINTReturnsToMenu(t *testing.T) {
	if os.Getenv("DEVBOX_TEST_TUI_SIGNAL_HELPER") == "1" {
		parent, timeout := context.WithTimeout(context.Background(), 10*time.Second)
		defer timeout()
		ctx, stop := SignalContext(parent)
		defer stop()
		e, _, name := namedCLIFixture(t)
		e.Streams = docker.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, TTY: true}
		e.Docker.Runner.(*dockertest.Daemon).Attached = func(ctx context.Context, c docker.Command) error {
			if c.Stdin == nil {
				return nil
			}
			fmt.Fprintln(c.Stdout, "WAITING FOR FOREGROUND SIGINT")
			<-ctx.Done()
			fmt.Fprintln(c.Stdout, "FOREGROUND CONTEXT CANCELLED")
			return ctx.Err()
		}
		cmd := &cobra.Command{Use: "devbox-neo"}
		cmd.SetContext(ctx)
		cmd.SetIn(os.Stdin)
		cmd.SetOut(os.Stdout)
		cmd.SetErr(os.Stderr)
		m := newMenu(cmd)
		f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		err := f.session(app.View{Name: name})
		finishErr := m.Finish()
		if err != nil || finishErr != nil || ctx.Err() != nil {
			t.Fatal("foreground SIGINT cancelled the browser", err, finishErr, ctx.Err())
		}
		fmt.Println("SIGNAL ROUTING VERIFIED")
		return
	}
	p := newTerminalProbe(t)
	command := exec.Command(os.Args[0], "-test.run=^TestForegroundSIGINTReturnsToMenu$", "-test.timeout=15s")
	command.Env = append(os.Environ(), "DEVBOX_TEST_TUI_SIGNAL_HELPER=1")
	command.Stdin, command.Stdout, command.Stderr = p.slave, p.slave, p.slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	p.wait("Shell")
	p.send("\x1b[B\x1b[B\x1b[B\r")
	p.wait("WAITING FOR FOREGROUND SIGINT")
	p.send("\x03")
	p.wait("Press Enter")
	p.send("\r")
	p.wait("Shell")
	p.send("q")
	p.finish(done)
	if !strings.Contains(p.output(), "SIGNAL ROUTING VERIFIED") {
		t.Fatal("helper did not complete", p.output())
	}
}
