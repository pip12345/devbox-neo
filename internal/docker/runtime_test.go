package docker_test

import (
	"context"
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func TestMountCSVPreservesLinuxPathCharacters(t *testing.T) {
	d := &dockertest.Daemon{}
	runtime := docker.Runtime{Runner: d}
	source := "/work/with,comma and\nnewline"
	plan := docker.CreatePlan{Name: "devbox-rewrite-test", Image: "image", Network: "default", Mounts: []docker.Mount{{Source: source, Target: "/workspace", ReadOnly: true}}}
	owner := docker.Owner{Installation: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Workspace: source, Slot: "project"}
	if _, err := runtime.Create(context.Background(), plan, owner); err != nil {
		t.Fatal(err)
	}
	args := d.History()[0]
	i := slices.Index(args, "--mount")
	if i < 0 {
		t.Fatal("missing mount")
	}
	fields, err := csv.NewReader(strings.NewReader(args[i+1])).Read()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fields, []string{"type=bind", "src=" + source, "dst=/workspace", "readonly"}) {
		t.Fatalf("mount was not encoded as one CSV record: %q", fields)
	}
}
