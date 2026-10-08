package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestFrontendResumesPinnedTransferWithoutChangingEndpoints(t *testing.T) {
	f, out, q, name := frontendFixture(t, strings.NewReader("1\n2\n3\n5\ny\n"))
	c, _, err := f.e.Docker.InspectID(context.Background(), sessionRecord(t, f.e, name).Applied.SetupContainer)
	if err != nil {
		t.Fatal(err)
	}
	sourceDirectory := sessionRecord(t, f.e, name).Directory
	daemon := f.e.Docker.Runner.(*dockertest.Daemon)
	daemon.Fail = func(args []string) error {
		if len(args) == 2 && args[0] == "rm" && args[1] == c.ID {
			return errors.New("interrupted source cleanup")
		}
		return nil
	}
	_, err = f.e.Transfer(context.Background(), app.TransferOptions{Source: name, Destination: q.Workspace, As: "Moved", Mode: "relocate"})
	if err == nil {
		t.Fatal("failure injection did not interrupt cleanup")
	}
	journal, err := f.e.Store.ReadTransfer(sourceDirectory)
	if err != nil || journal == nil || journal.Phase != "committed" {
		t.Fatal(journal, err)
	}
	daemon.Fail = nil
	moved, err := f.transfer(name)
	if err != nil || !moved {
		t.Fatal(moved, err, out.String())
	}
	if !strings.Contains(out.String(), "recorded endpoints and mode are pinned") {
		t.Fatal("pending transfer fields remained editable", out.String())
	}
	if journal, err = f.e.Store.ReadTransfer(sourceDirectory); err != nil || journal != nil {
		t.Fatal("transfer did not finish", journal, err)
	}
}

func TestNativeBrowserEnterResumesCurrentFolderSelection(t *testing.T) {
	for _, hasDefault := range []bool{false, true} {
		t.Run(fmt.Sprintf("default=%t", hasDefault), func(t *testing.T) {
			testNativeBrowserCurrentFolderSelection(t, hasDefault)
		})
	}
}

func testNativeBrowserCurrentFolderSelection(t *testing.T, hasDefault bool) {
	e, q, _ := namedCLIFixture(t)
	// The existing Main session's parent folder sorts before this workspace;
	// neither its position nor its ancestry should make it the initial target.
	q.Workspace = filepath.Join(q.Workspace, "project")
	if err := os.Mkdir(q.Workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	q.LocalName = "Zulu"
	zulu, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	q.LocalName = "Alpha"
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	initial, other, navigation := "Alpha", "Zulu", "\x1b[B\r"
	if hasDefault {
		if err := e.SetDefault(context.Background(), sessionRecord(t, e, zulu.SessionID)); err != nil {
			t.Fatal(err)
		}
		created = zulu
		initial, other, navigation = "Zulu", "Alpha", "\x1b[A\r"
	}
	targetContainer := sessionRecord(t, e, created.SessionID).Applied.SetupContainer
	attached := make(chan docker.Command, 1)
	e.Docker.Runner.(*dockertest.Daemon).Attached = func(_ context.Context, c docker.Command) error {
		if c.Stdin != nil {
			attached <- c
		}
		return nil
	}
	t.Chdir(q.Workspace)
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		e.Streams = docker.Streams{In: tty, Out: tty, Err: tty, TTY: true}
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		f := &frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.browse(false)
	})
	p.wait("Sessions")
	p.send("\r")
	p.wait("Session · " + initial)
	p.send("\r")
	p.wait("Press Enter")
	select {
	case c := <-attached:
		if !slices.Contains(c.Args, targetContainer) || c.Args[len(c.Args)-1] != "-c" {
			t.Fatal("first action did not continue the selected session's harness", c.Args)
		}
	default:
		t.Fatal("Enter did not launch the harness")
	}
	p.send("\r")
	p.wait("Session · " + initial)
	p.send("q")
	p.wait("Browser actions")
	// Returning from a different session must not reapply the startup highlight.
	p.send(navigation)
	p.wait("Session · " + other)
	p.send("q")
	p.wait("Browser actions")
	p.send("\r")
	p.wait("Session · " + other)
	p.send("q")
	p.wait("Browser actions")
	p.send("q")
	p.finish(done)
	selected, err := e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if hasDefault {
		if selected == nil || selected.ID != zulu.SessionID {
			t.Fatal("browser navigation changed the saved default", selected)
		}
	} else if selected != nil {
		t.Fatal("browser navigation selected a folder default", selected)
	}
}

func TestNativeFrontendHandsInputToAttachmentAndResumes(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	p := newTerminalProbe(t)
	before, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	daemon := e.Docker.Runner.(*dockertest.Daemon)
	daemon.Attached = func(ctx context.Context, c docker.Command) error {
		state, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
		if err != nil {
			return err
		}
		if *state != *before {
			return fmt.Errorf("attachment inherited TUI terminal mode")
		}
		if c.Stdin == nil {
			return nil
		} // Non-attached runtime preparation uses captured I/O.
		fmt.Fprintln(c.Stdout, "FOREGROUND INPUT READY")
		line, err := bufio.NewReader(c.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		if line != "owned by attachment\n" {
			return fmt.Errorf("terminal UI consumed attachment input: %q", line)
		}
		fmt.Fprintln(c.Stdout, "FOREGROUND INPUT COMPLETE")
		// Model a Docker client that exits without restoring raw terminal flags.
		raw := *state
		raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG
		raw.Iflag &^= unix.ICRNL
		raw.Oflag &^= unix.OPOST
		return unix.IoctlSetTermios(int(p.slave.Fd()), unix.TCSETS, &raw)
	}
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		e.Streams = docker.Streams{In: tty, Out: tty, Err: tty, TTY: true}
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		f := &frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.session(app.View{Target: name})
	})
	p.wait("Shell")
	p.send("\x1b[B\x1b[B\x1b[B\r")
	p.wait("FOREGROUND INPUT READY")
	p.send("owned by attachment\n")
	p.wait("Press Enter")
	restored, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || *restored != *before {
		t.Fatal("acknowledgement inherited raw child input", err)
	}
	p.send("\r")
	p.wait("Shell")
	p.send("q")
	p.finish(done)
	details, err := e.Status(context.Background(), name, "")
	if err != nil || len(details.Active) != 0 || details.Running {
		t.Fatal("attachment lease/lifetime cleanup changed", details, err)
	}
	after, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || *after != *before {
		t.Fatal("final terminal state not restored", err)
	}
	if !strings.Contains(p.output(), "FOREGROUND INPUT COMPLETE") {
		t.Fatal("attachment failed", p.output())
	}
}
