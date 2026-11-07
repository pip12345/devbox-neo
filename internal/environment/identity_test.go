package environment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestPublicNameUsesSessionIDAndSavedSettings(t *testing.T) {
	id := "a7e281de4b19"
	binding := Binding{Workspace: "/projects/hazeron", LocalName: "main"}
	name := PublicName(binding, id)
	if name != "dbx-hazeron-a7e281de4b19.main" {
		t.Fatal(name)
	}
	if got, ok := SessionIDFromName(name); !ok || got != id {
		t.Fatal(got, ok)
	}
	binding.Workspace = "/elsewhere/hazeron"
	if PublicName(binding, id) != name {
		t.Fatal("parent path became public identity")
	}
	binding.LocalName = "work"
	if PublicName(binding, id) != "dbx-hazeron-a7e281de4b19.work" {
		t.Fatal("label did not update public name")
	}
	binding.Workspace = "/projects/nova"
	if PublicName(binding, id) != "dbx-nova-a7e281de4b19.work" {
		t.Fatal("workspace hint did not update")
	}
	for _, bad := range []string{id, "dbx-hazeron-723b46117d2c", "dbx-hazeron-not-an-id.main", name + "/"} {
		if _, ok := SessionIDFromName(bad); ok {
			t.Fatal("invalid public name", bad)
		}
	}
}

func TestRuntimeNamesCaptureReadableHintsAndShortAttemptSuffix(t *testing.T) {
	id := "9833cab43e72"
	allocation := "2c7d3144cfe4f289cc8d3899b46ab700"
	binding := Binding{Workspace: "/projects/hazeron", LocalName: "main"}
	name := ResourceName(binding, id, allocation)
	if name != "dbx-hazeron-9833cab43e72.main-2c7d3144" {
		t.Fatal(name)
	}
	if name == ResourceName(binding, id, strings.Repeat("f", 32)) || name == ResourceName(binding, strings.Repeat("b", 12), allocation) {
		t.Fatal("runtime instances collided")
	}
	changed := Binding{Workspace: "/projects/other", LocalName: "work"}
	if name == ResourceName(changed, id, allocation) {
		t.Fatal("creation hints did not follow captured settings")
	}
	if name == PublicName(binding, id) {
		t.Fatal("runtime name lacks an instance suffix")
	}
	// Docker hints and their shortened suffix cannot alter the image alias.
	tag := ImageTag(id, allocation)
	if tag != "dbx/session:"+id+"-"+allocation || !config.ImageReference.MatchString(tag) {
		t.Fatal(tag)
	}
	for _, workspace := range []string{"/work/My Folder!", "/work/💥", "/work/" + strings.Repeat("x", 200)} {
		binding := Binding{Workspace: workspace, LocalName: strings.Repeat("N", 64)}
		name := PublicName(binding, id)
		if len(name) > 106 || len(ResourceName(binding, id, allocation)) > 115 {
			t.Fatal("unbounded readable hint", name)
		}
		if key, ok := SessionIDFromName(name); !ok || key != id {
			t.Fatal(name)
		}
	}
}

func TestWorkspaceSelectionCanonicalizesSymlinksWithoutAllocatingNames(t *testing.T) {
	workspace := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	a, err := Identify(workspace, "work")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Identify(alias, "work")
	if err != nil || a.Binding != b.Binding || a.Name != "" || b.Name != "" {
		t.Fatal(a, b, err)
	}
	a.Name = ResourceName(a.Binding, strings.Repeat("a", 12), strings.Repeat("b", 32))
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}
