//go:build integration

package app

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func TestDockerLayeredBuildCache(t *testing.T) {
	if os.Getenv("DEVBOX_DOCKER_TEST") != "1" {
		t.Skip("run make test-integration to opt in to isolated Docker work")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("real-Docker acceptance requires the Docker CLI and a reachable daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := docker.ExecRunner{}
	if err := runner.Run(ctx, docker.Command{Args: []string{"info"}}); err != nil {
		t.Fatalf("Docker unavailable: %v", err)
	}
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	output := &integrationOutput{logf: t.Logf}
	e := &Engine{Store: s, Docker: docker.Runtime{Runner: runner}, Streams: docker.Streams{Out: output, Err: output}}
	var tags []string
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, tag := range tags {
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
	stage := func(name string) []byte {
		text := "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n"
		if name == "prepared" {
			text = "FROM debian:13-slim\n"
		}
		if name == "harness" {
			text += "ARG " + environment.HarnessRefreshArg + "\n"
		}
		// Each execution writes different bytes, so equal layers prove cache reuse.
		// The private installation ID keeps other tests' cache out of this probe.
		text += "RUN cat /proc/sys/kernel/random/uuid > /tmp/dbx-cache-" + s.Installation + "-" + name + "\n"
		return []byte(text)
	}
	spec := environment.Spec{Build: environment.ImageBuildPlan{
		Prepared: stage("prepared"),
		Stages: []environment.ImageStage{
			{Source: "first cache-test stage", Dockerfile: stage("first")},
			{Source: "second cache-test stage", Dockerfile: stage("second")},
		},
		Boundary:  stage("boundary"),
		Runtime:   stage("harness"),
		Arguments: map[string]string{},
	}}
	build := func(name string, force bool, token string) docker.Image {
		t.Helper()
		t.Log("build:", name)
		tag := docker.Namespace + "/cache-test:" + s.Installation + "-" + name
		tags = append(tags, tag)
		image, err := e.build(ctx, spec, tag, force, token)
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, output.String())
		}
		return image
	}
	first := build("first", false, "refresh-one")
	repeated := build("repeated", false, "refresh-one")
	if !slices.Equal(first.RootFS.Layers, repeated.RootFS.Layers) {
		t.Fatal("identical builds with one refresh token did not reuse every layer", first.RootFS.Layers, repeated.RootFS.Layers)
	}
	refreshed := build("refreshed", false, "refresh-two")
	last := len(first.RootFS.Layers) - 1
	if len(refreshed.RootFS.Layers) != len(first.RootFS.Layers) || last < 4 {
		t.Fatal("unexpected controlled layer count", first.RootFS.Layers, refreshed.RootFS.Layers)
	}
	if !slices.Equal(first.RootFS.Layers[:last], refreshed.RootFS.Layers[:last]) || first.RootFS.Layers[last] == refreshed.RootFS.Layers[last] {
		t.Fatal("a new refresh token must rebuild only the installation layer", first.RootFS.Layers, refreshed.RootFS.Layers)
	}
	uncached := build("uncached", true, "")
	if len(uncached.RootFS.Layers) != len(first.RootFS.Layers) {
		t.Fatal("unexpected uncached layer count", uncached.RootFS.Layers)
	}
	for i := len(first.RootFS.Layers) - 5; i < len(first.RootFS.Layers); i++ {
		if first.RootFS.Layers[i] == uncached.RootFS.Layers[i] {
			t.Fatal("full rebuild reused a controlled layer", i, uncached.RootFS.Layers)
		}
	}
}
