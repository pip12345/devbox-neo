package environment

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestResourceNamesUseReadableHintsAndAnAllocationSuffix(t *testing.T) {
	a := ResourceName("/work/api", "Main", "allocation-one")
	if a != ResourceName("/moved/api", "Main", "allocation-one") {
		t.Fatal("workspace path became resource identity")
	}
	if a == ResourceName("/work/api", "Main", "allocation-two") {
		t.Fatal("reused settings collided with an old resource")
	}
	if !strings.HasPrefix(a, "devbox-api-") || !strings.HasSuffix(a, ".Main") {
		t.Fatal(a)
	}
}

func TestResourceNameHintsAreSafeAndBounded(t *testing.T) {
	valid := regexp.MustCompile(`^devbox-[a-z0-9_.-]+-[a-f0-9]{12}\.project$`)
	for _, folder := range []string{"My App", "my.app_v2", "--My App..", "a ?\t\n b", "日本語", "...", strings.Repeat("a", 80), "/"} {
		name := ResourceName(filepath.Join("/parent", folder), "project", "allocation")
		if !valid.MatchString(name) || len(name) > 117 {
			t.Fatal(folder, name)
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
