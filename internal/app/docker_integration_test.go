//go:build integration

package app

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/harness"
	"devbox/internal/resource"
	"devbox/internal/store"
)

func TestDockerPiLifecycle(t *testing.T)       { dockerHarnessLifecycle(t, "pi") }
func TestDockerOpenCodeLifecycle(t *testing.T) { dockerHarnessLifecycle(t, "opencode") }
func TestDockerCustomLifecycle(t *testing.T)   { dockerHarnessLifecycle(t, "third") }

func dockerHarnessLifecycle(t *testing.T, harnessName string) {
	if os.Getenv("DEVBOX_DOCKER_TEST") != "1" {
		t.Skip("run make test-integration to opt in to isolated Docker work")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("real-Docker acceptance requires the Docker CLI and a reachable daemon")
	}
	if os.Getuid() == 0 || os.Getgid() == 0 {
		t.Fatal("run this Linux integration test as a non-root user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	runner := docker.ExecRunner{}
	if err := runner.Run(ctx, docker.Command{Args: []string{"info"}}); err != nil {
		t.Fatalf("Docker unavailable: %v", err)
	}
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if harnessName == "third" {
		seedThird(t, s.Home)
	}
	resources := resource.Service{Home: s.Home}
	profile, err := resources.ConfigDirectory("test", workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resources.CreateConfig(ctx, profile, resource.SetupOptions{Harness: &harnessName}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	e := &Engine{Store: s, Docker: docker.Runtime{Runner: runner}, Streams: docker.Streams{Out: &output, Err: &output}, UID: os.Getuid(), GID: os.Getgid()}
	e.OnDiagnostic = func(d Diagnostic) { t.Logf("diagnostic: %+v", d) }
	q := Request{Workspace: workspace, LocalName: "test", Sources: []config.Reference{{Label: "base", Kind: config.ReferenceFixed, Path: profile.Root}}, Args: []string{"--version"}}
	var createdID string
	// The isolated installation is the cleanup boundary, never a name prefix.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		containers, err := e.Docker.Inventory(cleanup, s.Installation)
		if err != nil {
			t.Error(err)
			return
		}
		ids := map[string]bool{}
		if createdID != "" {
			ids[createdID] = true
		}
		for _, c := range containers {
			owner, err := e.orphanOwner(c)
			if err != nil {
				t.Error(err)
				continue
			}
			ids[owner.Session] = true
			if c.State.Running {
				if err := e.Docker.Stop(cleanup, c, owner); err != nil {
					t.Error(err)
					continue
				}
			}
			if err := e.Docker.Remove(cleanup, c, owner); err != nil {
				t.Error(err)
			}
		}
		for id := range ids {
			tag := docker.Namespace + "/session:" + id
			image, exists, err := e.Docker.TaggedImage(cleanup, tag)
			if err != nil {
				t.Error(err)
				continue
			}
			if exists {
				if err := e.Docker.Untag(cleanup, tag, image.ID, s.Installation); err != nil {
					t.Error(err)
				}
			}
		}
	})
	created, err := e.Create(ctx, q)
	createdID = created.SessionID
	if err != nil {
		t.Fatalf("create: %v\n%s", err, output.String())
	}
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatalf("open: %v\n%s", err, output.String())
	}
	first := sessionRecord(t, e, result.SessionID)
	output.Reset()
	if harnessName == "opencode" {
		if err := e.Exec(ctx, result.SessionID, "", []string{"opencode", "auth", "export"}, false); err != nil {
			t.Fatalf("OpenCode credential API/wrapper: %v\n%s", err, output.String())
		}
		if strings.TrimSpace(output.String()) != "[]" {
			t.Fatal("fresh OpenCode auth must be an empty shared credential snapshot")
		}
		output.Reset()
	}
	if err := e.Exec(ctx, result.SessionID, "test", []string{"bash", "-ic", `set -eu; for tool in vim zip unzip jq ifconfig ping; do command -v "$tool"; done; alias ll; alias vi`}, false); err != nil {
		t.Fatalf("bundled tool check: %v\n%s", err, output.String())
	}
	for _, alias := range []string{"alias ll='ls -alF'", "alias vi='vim'"} {
		if !strings.Contains(output.String(), alias) {
			t.Fatalf("missing Bash alias %q: %s", alias, output.String())
		}
	}
	// Probe sibling-directory creation as the normal container user in both
	// image-owned and bind-backed parents. Version-only launches can miss this
	// for custom harnesses that do not initialize state on startup.
	parents := (harness.Definition{Stores: first.Applied.Stores, Auth: first.Applied.Auth}).MountParents()
	argv := []string{"/bin/sh", "-eu", "-c", `for parent do probe=$(mktemp -d "$parent/.devbox-parent-XXXXXX"); rmdir -- "$probe"; done`, "mount-parent-check"}
	for _, parent := range parents {
		argv = append(argv, parent.Target)
	}
	if err = e.Exec(ctx, result.SessionID, "", argv, false); err != nil {
		t.Fatalf("mount parent contract: %v\n%s", err, output.String())
	}
	if err = e.Exec(ctx, result.SessionID, "", []string{"sh", "-c", `test -r /devbox/AGENTS.md && test -r /devbox/docs/index.md && test -r /devbox/network/inspect.json && test ! -w /devbox/docs/index.md && . /devbox/network/env && test -n "$DEVBOX_HOST"`}, false); err != nil {
		t.Fatal("runtime docs/network contract", err)
	}
	e.TerminalEnv = []string{"TERM=xterm-256color", "COLORTERM=truecolor"}
	if err = e.Exec(ctx, result.SessionID, "", []string{"sh", "-c", `test "$TERM" = xterm-256color && test "$COLORTERM" = truecolor`}, false); err != nil {
		t.Fatal("existing-container terminal forwarding", err)
	}
	marker := ""
	for _, declared := range first.Applied.Stores {
		if declared.Scope == "environment" {
			marker = filepath.Join(s.Home, "sessions", first.Directory, "harnesses", harnessName, "stores", declared.Name, "preservation-check")
			break
		}
	}
	if marker == "" {
		t.Fatal("fixture has no environment store")
	}
	write(t, marker, "preserved")
	authPaths := []string{}
	for _, auth := range first.Applied.Auth {
		target := auth.Target
		source := filepath.Join(s.Home, "auth", harnessName, auth.Source)
		if auth.Kind == "directory" {
			target += "/acceptance.json"
			source = filepath.Join(source, "acceptance.json")
		}
		if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
			t.Fatal(err)
		}
		if err = e.Exec(ctx, result.SessionID, "", []string{"bash", "-c", `printf '{}\n\n' > "$1"`, "auth-check", target}, false); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(source); err != nil || string(b) != "{}\n\n" {
			t.Fatal("in-container auth write did not persist on host", err)
		}
		authPaths = append(authPaths, source)
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	reopened := sessionRecord(t, e, result.SessionID)
	if reopened.Applied.SetupContainer != first.Applied.SetupContainer {
		t.Fatal("reopen recreated container")
	}
	socketDir, err := os.MkdirTemp("", "dbx-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "service.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "socket-mount-ok")
	})}
	t.Cleanup(func() { server.Close() })
	go server.Serve(listener)
	// Configured mounts are creation inputs but do not require another image.
	write(t, filepath.Join(profile.Root, "config.json"), fmt.Sprintf(`{"version":1,"harness":%q,"mounts":[%q,%q]}`, harnessName, workspace+":/extra-workspace", socketPath+":/run/dbx-test.sock"))
	result, err = e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "creation_drift" {
		t.Fatal("missing non-blocking drift warning")
	}
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	recreated := sessionRecord(t, e, result.SessionID)
	if recreated.ID != first.ID || recreated.Applied.SetupContainer == first.Applied.SetupContainer {
		t.Fatal("wrong recreation identity")
	}
	output.Reset()
	if err = e.Exec(ctx, result.SessionID, "", []string{"curl", "--fail", "--silent", "--max-time", "10", "--unix-socket", "/run/dbx-test.sock", "http://localhost/"}, false); err != nil || !strings.Contains(output.String(), "socket-mount-ok") {
		t.Fatalf("socket bind connection: %v\n%s", err, output.String())
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(b)) != "preserved" {
		t.Fatal("recreation lost session state")
	}
	for _, path := range authPaths {
		if b, err := os.ReadFile(path); err != nil || string(b) != "{}\n\n" {
			t.Fatal("recreation lost managed auth", err)
		}
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	if err = e.Exec(ctx, result.SessionID, "", argv, false); err != nil {
		t.Fatalf("mount parents after recreate/start: %v\n%s", err, output.String())
	}
}
