package sshshare

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDestinationValidation(t *testing.T) {
	for _, s := range []string{"staging", "user@server", "user@192.168.1.2", "user@2001:db8::1", "::1"} {
		if err := Validate(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"", "-oProxyCommand=bad", "host\nHost *", "user@host command", "foo/bar", "*.example", "host%h", "a@b@c"} {
		if Validate(s) == nil {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestConnectionsReservePublishFailClosedAndDoNotCopyCredentials(t *testing.T) {
	root := t.TempDir()
	first, err := Prepare(root, "staging")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if first.Alias != "staging" {
		t.Fatal(first.Alias)
	}
	if _, err := Prepare(root, "staging"); err == nil {
		t.Fatal("duplicate accepted")
	}
	second, err := Prepare(root, "user@server")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Alias == first.Alias {
		t.Fatal("alias collision")
	}
	if _, err := os.Stat(filepath.Join(root, first.Relative, "config")); !os.IsNotExist(err) {
		t.Fatal("published before authentication")
	}
	if err := first.Publish(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Include /devbox/ssh/c/*/config", "BatchMode yes", "ControlMaster no", "ProxyCommand /bin/false"} {
		if !strings.Contains(string(b), want) {
			t.Fatal(string(b))
		}
	}
	if strings.Contains(Supervisor, "ForwardAgent") || strings.Contains(Supervisor, "ForwardX11") || strings.Contains(Supervisor, "StrictHostKeyChecking") {
		t.Fatal("master overrides user's SSH policy")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, first.Relative, "config")); !os.IsNotExist(err) {
		t.Fatal("connection remained published")
	}
	retry, err := Prepare(root, "staging")
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Close()
	if retry.Relative == first.Relative {
		t.Fatal("reused controller inode path")
	}
	if _, err := Prepare(root, "user@server"); err == nil {
		t.Fatal("closing first disrupted second")
	}
}

// The helper is a real subprocess with a Unix listener, exercising the shell
// supervisor and kernel flock lifetime without contacting any SSH server.
func TestSSHHelper(t *testing.T) {
	if os.Getenv("DEVBOX_SSH_HELPER") != "1" {
		return
	}
	args := os.Args
	var socket string
	for i, a := range args {
		if a == "-S" && i+1 < len(args) {
			socket = args[i+1]
		}
	}
	if socket == "" {
		os.Exit(90)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(socket), "pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(91)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(94)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(socket), "cwd"), []byte(cwd), 0600); err != nil {
		os.Exit(95)
	}
	if os.Getenv("DEVBOX_SSH_AUTH_FAILURE") == "1" {
		os.Exit(255)
	}
	if os.Getenv("DEVBOX_SSH_AUTH_WAIT") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(92)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(93)
		}
		conn.Close()
	}
}

func fakeSSH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "' -test.run=^TestSSHHelper$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("DEVBOX_SSH_HELPER", "1")
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestSupervisorClosesActualMasterOnControllerLoss(t *testing.T) {
	for _, authenticating := range []bool{false, true} {
		t.Run(strconv.FormatBool(authenticating), func(t *testing.T) {
			fakeSSH(t)
			if authenticating {
				t.Setenv("DEVBOX_SSH_AUTH_WAIT", "1")
			}
			c, err := Prepare(t.TempDir(), "staging")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- RunHost(ctx, c, nil, io.Discard, io.Discard) }()
			var pid int
			eventually(t, func() bool {
				b, err := os.ReadFile(filepath.Join(c.Root, c.Relative, "pid"))
				if err != nil {
					return false
				}
				pid, _ = strconv.Atoi(string(b))
				return pid > 0
			})
			if !authenticating {
				eventually(t, c.Ready)
				if err := c.Publish(); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate an abruptly lost controller, not a cooperative SSH -O exit.
			if err := c.owner.Close(); err != nil {
				t.Fatal(err)
			}
			c.owner = nil
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Fatal("orphaned supervisor")
			}
			eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) })
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHostSSHExitStatus(t *testing.T) {
	fakeSSH(t)
	t.Setenv("DEVBOX_SSH_AUTH_FAILURE", "1")
	c, err := Prepare(t.TempDir(), "staging")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = RunHost(context.Background(), c, nil, io.Discard, io.Discard)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 255 {
		t.Fatal(err)
	}
}

func TestLongHomeAndDeadSocket(t *testing.T) {
	fakeSSH(t)
	root := filepath.Join(t.TempDir(), strings.Repeat("long", 40))
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := Prepare(root, "staging")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunHost(ctx, c, nil, io.Discard, io.Discard) }()
	eventually(t, c.Ready)
	cwd, _ := os.Getwd()
	actual, err := os.ReadFile(filepath.Join(c.Root, c.Relative, "cwd"))
	if err != nil || string(actual) != cwd {
		t.Fatalf("host SSH changed working directory: %q %v", actual, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if c.Ready() {
		t.Fatal("dead socket reported ready")
	}
}

func TestGeneratedDefaultsDoNotAuthenticateWhenUnavailable(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH not installed")
	}
	root := t.TempDir()
	c, err := Prepare(root, "staging")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// No /devbox mount is needed to prove that the generated catch-all rejects
	// unknown and unavailable aliases rather than trying DNS/network/authentication.
	output, err := exec.CommandContext(ctx, ssh, "-F", filepath.Join(root, "config"), "staging", "true").CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatalf("did not fail promptly: %v %s", err, output)
	}
	if strings.Contains(string(output), "password:") || strings.Contains(string(output), "Could not resolve") {
		t.Fatalf("attempted normal connection: %s", output)
	}
}
