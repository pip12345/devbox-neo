package docker_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func TestRecordingRunner(t *testing.T) {
	r := &dockertest.Runner{Steps: []dockertest.Step{{Args: []string{"version"}, Output: "fake"}}}
	out := new(bytes.Buffer)
	if err := r.Run(context.Background(), docker.Command{Args: []string{"version"}, Stdout: out}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "fake" || len(r.Calls) != 1 || len(r.Steps) != 0 {
		t.Fatal("recording contract")
	}
}
func TestForegroundExit(t *testing.T) {
	err := (docker.ExecRunner{Binary: "sh"}).Run(context.Background(), docker.Command{Args: []string{"-c", "exit 23"}})
	var exit *docker.ExitError
	if !errors.As(err, &exit) || exit.Code != 23 {
		t.Fatalf("exit status: %v", err)
	}
}
