package docker_test

import (
	"context"
	"encoding/json"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func TestAllContainersIncludesUnmanagedMounts(t *testing.T) {
	c := docker.Container{ID: "container-id", Name: "/external", Mounts: []docker.ContainerMount{{Type: "bind", Source: "/state/session", Destination: "/data"}}}
	b, err := json.Marshal([]docker.Container{c})
	if err != nil {
		t.Fatal(err)
	}
	runner := &dockertest.Runner{Steps: []dockertest.Step{
		{Args: []string{"container", "ls", "--all", "--no-trunc", "--format", "{{.ID}}"}, Output: c.ID},
		{Args: []string{"container", "inspect", c.ID}, Output: string(b)},
	}}
	containers, err := (docker.Runtime{Runner: runner}).AllContainers(context.Background())
	if err != nil || len(containers) != 1 || len(containers[0].Mounts) != 1 || containers[0].Mounts[0] != c.Mounts[0] {
		t.Fatal(containers, err)
	}
}

func TestContainerInventoryRejectsChangedInspection(t *testing.T) {
	for _, output := range []string{`[]`, `[{"Id":"different-id"}]`, `[{"Id":"expected-id"},{"Id":"expected-id"}]`} {
		runner := &dockertest.Runner{Steps: []dockertest.Step{
			{Args: []string{"container", "ls", "--all", "--no-trunc", "--format", "{{.ID}}"}, Output: "expected-id"},
			{Args: []string{"container", "inspect", "expected-id"}, Output: output},
		}}
		if _, err := (docker.Runtime{Runner: runner}).AllContainers(context.Background()); err == nil {
			t.Fatal("incomplete/changed inventory accepted", output)
		}
	}
}
