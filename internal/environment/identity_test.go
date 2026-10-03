package environment

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"devbox/internal/config"
	"testing"
)

func TestResourceNamesUseCompactReadableHintsAndAnAllocationSuffix(t *testing.T) {
	a := ResourceName("/work/api", "Main", "allocation-one")
	if a != ResourceName("/moved/api", "Main", "allocation-one") {
		t.Fatal("workspace path became resource identity")
	}
	if a == ResourceName("/work/api", "Main", "allocation-two") {
		t.Fatal("reused settings collided with an old resource")
	}
	if !regexp.MustCompile(`^dbx-api-[a-f0-9]{12}\.Main$`).MatchString(a) {
		t.Fatal(a)
	}
}

func TestResourceHintsAndImageTagsAreBounded(t *testing.T) {
	id := strings.Repeat("a", 32)
	local := strings.Repeat("N", 64)
	for _, workspace := range []string{"/work/My Folder!", "/work/💥", "/work/" + strings.Repeat("x", 200)} {
		name := ResourceName(workspace, local, id)
		if !regexp.MustCompile(`^dbx-[a-z0-9_.-]+-[a-f0-9]{12}\.[A-Za-z0-9_-]+$`).MatchString(name) {
			t.Fatal(name)
		}
		tag := ImageTag(workspace, local, id)
		if !config.ImageReference.MatchString(tag) || len(strings.SplitN(tag, ":", 2)[1]) > 128 {
			t.Fatal(tag)
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
	// A recorded resource name need not describe its current binding.
	a.Name = ResourceName("/old/elsewhere", "old", "allocation")
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}
