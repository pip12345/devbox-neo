//go:build integration

package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func TestDockerPiLifecycle(t *testing.T) {
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
	write(t, filepath.Join(s.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi"}`)
	var output bytes.Buffer
	e := &Engine{Store: s, Docker: docker.Runtime{Runner: runner}, Streams: docker.Streams{Out: &output, Err: &output}, UID: os.Getuid(), GID: os.Getgid()}
	q := Request{Workspace: workspace, Profile: "test", Args: []string{"--version"}}
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup is limited to this unique installation and exact test identity. It
	// never uses a prefix or a global prune command as proof of ownership.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		l, err := s.Lock(cleanup, spec.Identity.Name)
		if err != nil {
			t.Error(err)
			return
		}
		defer l.Close()
		c, exists, err := e.Docker.Inspect(cleanup, spec.Identity.Name)
		if err != nil {
			t.Error(err)
			return
		}
		var sessionID string
		if exists {
			sessionID = c.Config.Labels[docker.Namespace+".session"]
			owner := docker.Owner{Installation: s.Installation, Session: sessionID, Workspace: workspace, Slot: spec.Identity.Slot}
			if err = c.Verify(owner); err != nil {
				t.Error(err)
				return
			}
			if c.State.Running {
				if err = e.Docker.Stop(cleanup, c, owner); err != nil {
					t.Error(err)
					return
				}
			}
			if err = e.Docker.Remove(cleanup, c, owner); err != nil {
				t.Error(err)
				return
			}
		}
		if r, err := l.Load(); err == nil {
			sessionID = r.ID
		}
		if sessionID != "" {
			tag := docker.Namespace + "/session:" + sessionID
			image, err := e.Docker.InspectImage(cleanup, tag)
			if err != nil {
				t.Error(err)
				return
			}
			if err = image.Verify(s.Installation); err != nil {
				t.Error(err)
				return
			}
			if err = runner.Run(cleanup, docker.Command{Args: []string{"image", "rm", tag}}); err != nil {
				t.Error(err)
			}
		}
	})
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatalf("create/open: %v\n%s", err, output.String())
	}
	first := record(t, e, result.Name)
	marker := filepath.Join(s.Home, "sessions", result.Name, "harnesses/pi/stores/home/sessions/preservation-check")
	write(t, marker, "preserved")
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	reopened := record(t, e, result.Name)
	if reopened.SetupContainer != first.SetupContainer {
		t.Fatal("reopen recreated container")
	}
	// Read-only workspace is a real creation input but does not require another image.
	q.ReadOnly = true
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
	recreated := record(t, e, result.Name)
	if recreated.ID != first.ID || recreated.SetupContainer == first.SetupContainer {
		t.Fatal("wrong recreation identity")
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(b)) != "preserved" {
		t.Fatal("recreation lost session state")
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
}
