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

func TestRuntimeNamesAndImageAliasesDoNotDependOnSettings(t *testing.T) {
	id := strings.Repeat("a", 12)
	a := ResourceName(id, "attempt-one")
	if a == ResourceName(id, "attempt-two") || a == ResourceName(strings.Repeat("b", 12), "attempt-one") {
		t.Fatal("runtime instances collided")
	}
	if _, ok := SessionIDFromName(a); ok {
		t.Fatal("runtime name became public session target")
	}
	if tag := ImageTag(a); !config.ImageReference.MatchString(tag) || len(strings.SplitN(tag, ":", 2)[1]) > 128 {
		t.Fatal(tag)
	}
	for _, workspace := range []string{"/work/My Folder!", "/work/💥", "/work/" + strings.Repeat("x", 200)} {
		name := PublicName(Binding{Workspace: workspace, LocalName: strings.Repeat("N", 64)}, id)
		if len(name) > 106 {
			t.Fatal("unbounded public hint", name)
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
	a.Name = ResourceName(strings.Repeat("a", 12), "attempt")
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}
