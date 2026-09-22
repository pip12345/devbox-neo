package docker_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func TestExecForwardsEnvWithoutChangingArgvOrStreams(t *testing.T) {
	d := &dockertest.Daemon{}
	runtime := docker.Runtime{Runner: d}
	owner := docker.Owner{Installation: "installation", Session: "session", Workspace: "/workspace", LocalName: "project"}
	c := docker.Container{ID: "container-id"}
	c.Config.Labels = owner.Labels()
	streams := docker.Streams{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, TTY: true}
	d.Attached = func(_ context.Context, command docker.Command) error {
		if command.Stdin != streams.In || command.Stdout != streams.Out || command.Stderr != streams.Err {
			t.Fatal("attachment streams changed")
		}
		return nil
	}
	argv := []string{"bash", "-c", "printf '%s' \"$TERM\""}
	env := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR=", "TERM_PROGRAM=name with spaces"}
	if err := runtime.Exec(context.Background(), c, owner, argv, env, streams); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--user", "devuser", "--workdir", "/workspace", "--interactive", "--tty"}
	for _, entry := range env {
		want = append(want, "--env", entry)
	}
	want = append(append(want, c.ID), argv...)
	if got := d.History()[0]; !slices.Equal(got, want) {
		t.Fatalf("exec = %q, want %q", got, want)
	}
	for _, bad := range [][]string{{"TERM=bad\nvalue"}, {"NO_COLOR"}, {"TERM=bad\x00value"}} {
		if err := runtime.Exec(context.Background(), c, owner, argv, bad, streams); err == nil {
			t.Fatal("invalid env accepted", bad)
		}
	}
	c.Config.Labels = nil
	if err := runtime.Exec(context.Background(), c, owner, argv, env, streams); err == nil {
		t.Fatal("env forwarding bypassed ownership verification")
	}
	if len(d.History()) != 1 {
		t.Fatal("invalid execution reached Docker")
	}
}
