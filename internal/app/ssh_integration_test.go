//go:build integration

package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

type sshTestOutput struct {
	sync.Mutex
	data bytes.Buffer
}

func (b *sshTestOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.data.Write(p)
}
func (b *sshTestOutput) String() string { b.Lock(); defer b.Unlock(); return b.data.String() }

// The only SSH server is inside an installation-labelled disposable fixture.
// Both hops use generated fixture keys and explicit known-host entries; no host
// SSH config, credentials, daemon, ports, or existing Devbox resources are used.
func TestDockerSSHMastersThroughProxyJump(t *testing.T) {
	if os.Getenv("DEVBOX_DOCKER_TEST") != "1" {
		t.Skip("run make test-integration")
	}
	if os.Getuid() == 0 || os.Getgid() == 0 {
		t.Fatal("run Docker SSH integration as a non-root Linux user")
	}
	for _, binary := range []string{"docker", "ssh", "ssh-keygen", "flock"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	state, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedThird(t, state.Home)
	definition := strings.Replace(thirdDefinition, `"shell":""`, `"shell":"sudo apt-get update && sudo apt-get install -y openssh-server && sudo mkdir -p /run/sshd && sudo usermod -p x devuser"`, 1)
	write(t, filepath.Join(state.Home, "harnesses/third/harness.json"), definition)
	write(t, filepath.Join(state.Home, "profiles/test/config.json"), `{"version":1,"harness":"third","on_exit":"running"}`)
	output := new(sshTestOutput)
	e := &Engine{Store: state, Docker: docker.Runtime{Runner: docker.ExecRunner{}}, Streams: docker.Streams{Out: output, Err: output}, UID: os.Getuid(), GID: os.Getgid()}
	result, err := e.Create(ctx, Request{Workspace: t.TempDir(), Profile: "test"})
	if err != nil {
		t.Fatalf("create: %v\n%s", err, output.String())
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if _, err := e.Delete(cleanup, DeleteOptions{Selection: Selection{Targets: []string{result.Name}}, Scope: DeleteSession}); err != nil {
			t.Error(err)
		}
	})
	if _, err = e.Start(ctx, result.Name, ""); err != nil {
		t.Fatal(err)
	}
	r := record(t, e, result.Name)
	c, _, err := e.inspect(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	ip := c.NetworkSettings.Networks["bridge"].IPAddress
	if ip == "" {
		t.Fatal("missing default bridge IP")
	}
	fixture := t.TempDir()
	login := filepath.Join(fixture, "login")
	if b, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", login).CombinedOutput(); err != nil {
		t.Fatalf("fixture key: %v %s", err, b)
	}
	if err := e.Docker.Runner.Run(ctx, docker.Command{Args: []string{"cp", fixture + "/.", c.ID + ":/tmp/ssh-fixture"}}); err != nil {
		t.Fatal(err)
	}
	serverConfig := "Port 2222\nPort 2223\nListenAddress 0.0.0.0\nHostKey /etc/ssh/ssh_host_ed25519_key\nPidFile /tmp/ssh-fixture/sshd.pid\nAuthorizedKeysFile /home/devuser/.ssh/authorized_keys\nPasswordAuthentication no\nUsePAM no\nAllowUsers devuser\n"
	setup := `sudo chown -R devuser /tmp/ssh-fixture
chmod 700 /tmp/ssh-fixture
chmod 600 /tmp/ssh-fixture/login
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
install -m 600 /tmp/ssh-fixture/login.pub "$HOME/.ssh/authorized_keys"
printf '%s' "$1" > /tmp/ssh-fixture/sshd_config
sudo /usr/sbin/sshd -f /tmp/ssh-fixture/sshd_config
sudo cat /etc/ssh/ssh_host_ed25519_key.pub`
	var public bytes.Buffer
	if err := e.Docker.Exec(ctx, c, e.owner(r), []string{"bash", "-c", setup, "fixture", serverConfig}, nil, docker.Streams{Out: &public, Err: output}); err != nil {
		t.Fatalf("sshd: %v %s", err, output.String())
	}
	known := "fixture " + strings.TrimSpace(public.String()) + "\n"
	write(t, filepath.Join(fixture, "known_hosts"), known)
	config := func(host, key, known string) string {
		return fmt.Sprintf("Host target\n  Port 2223\n  ProxyJump jump\nHost jump\n  Port 2222\nHost *\n  HostName %s\n  User devuser\n  IdentityFile %s\n  IdentityAgent none\n  IdentitiesOnly yes\n  HostKeyAlias fixture\n  StrictHostKeyChecking yes\n  UserKnownHostsFile %s\n", host, key, known)
	}
	containerConfig := config("127.0.0.1", "/tmp/ssh-fixture/login", "/tmp/ssh-fixture/known_hosts")
	configure := `mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
printf '%s' "$1" > "$HOME/.ssh/config"
printf '%s' "$2" > /tmp/ssh-fixture/known_hosts
chmod 600 "$HOME/.ssh/config"`
	if err := e.Docker.Exec(ctx, c, e.owner(r), []string{"bash", "-c", configure, "fixture", containerConfig, known}, nil, docker.Streams{Err: output}); err != nil {
		t.Fatal(err)
	}
	hostConfig := filepath.Join(fixture, "config")
	write(t, hostConfig, config(ip, login, filepath.Join(fixture, "known_hosts")))
	// The wrapper selects an isolated host SSH config without adding a production
	// credential/config-copy flag or relying on HOME (OpenSSH uses passwd homes).
	realSSH, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(fixture, "bin")
	if err := os.Mkdir(wrapper, 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	write(t, filepath.Join(wrapper, "ssh"), "#!/bin/sh\nexec "+quote(realSSH)+" -F "+quote(hostConfig)+" \"$@\"\n")
	if err := os.Chmod(filepath.Join(wrapper, "ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+":"+os.Getenv("PATH"))
	for _, hostMaster := range []bool{false, true} {
		t.Run(fmt.Sprintf("host=%v", hostMaster), func(t *testing.T) {
			connectionCtx, end := context.WithCancel(ctx)
			ready := make(chan string, 1)
			done := make(chan error, 1)
			stopped := make(chan struct{})
			defer func() {
				end()
				select {
				case <-stopped:
				case <-time.After(10 * time.Second):
					t.Error("SSH cleanup did not finish")
				}
			}()
			go func() {
				defer close(stopped)
				done <- e.SSH(connectionCtx, result.Name, "", "target", SSHOptions{HostMaster: hostMaster, Connected: func(_, alias string) { ready <- alias }})
			}()
			var alias string
			select {
			case alias = <-ready:
			case err := <-done:
				t.Fatalf("SSH startup: %v\n%s", err, output.String())
			case <-time.After(30 * time.Second):
				t.Fatalf("SSH timeout\n%s", output.String())
			}
			// Exec refreshes /devbox assets while the socket is live: it must not chown
			// or chmod the socket mount while installing documentation.
			if err := e.Exec(ctx, result.Name, "", []string{"ssh", "-F", "/devbox/ssh/config", alias, "printf ssh-fixture-ok"}, false); err != nil {
				t.Fatalf("shared command: %v\n%s", err, output.String())
			}
			if !strings.Contains(output.String(), "ssh-fixture-ok") {
				t.Fatal(output.String())
			}
			end()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("master did not end")
			}
			if err := e.Docker.Exec(ctx, c, e.owner(r), []string{"ssh", "-F", "/devbox/ssh/config", alias, "true"}, nil, docker.Streams{Err: output}); err == nil {
				t.Fatal("client reauthenticated after master closed")
			}
		})
	}
}
