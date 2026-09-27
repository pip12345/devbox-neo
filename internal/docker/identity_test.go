package docker_test

import (
	"context"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func TestInspectIDAndOwnershipDoNotDependOnNamesOrWorkspaceLabels(t *testing.T) {
	d := &dockertest.Daemon{}
	r := docker.Runtime{Runner: d}
	owner := docker.Owner{Installation: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Workspace: "/old/workspace", LocalName: "old"}
	id, err := r.Create(context.Background(), docker.CreatePlan{Name: "devbox-original", Image: "image", Network: "default"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := d.Snapshot("devbox-original")
	if !ok {
		t.Fatal("missing fixture")
	}
	d.Forget("devbox-original")
	c.Name = "/unrelated-runtime-name"
	c.Config.Labels[docker.Namespace+".workspace"] = "/another/workspace"
	c.Config.Labels[docker.Namespace+".local-name"] = "another"
	d.SetContainer(c)
	live, exists, err := r.InspectID(context.Background(), id)
	if err != nil || !exists || live.Name != c.Name || live.Verify(owner) != nil {
		t.Fatal(live, exists, err)
	}
	live.Config.Labels[docker.Namespace+".session"] = strings.Repeat("c", 32)
	if live.Verify(owner) == nil {
		t.Fatal("session ownership was not enforced")
	}
}
