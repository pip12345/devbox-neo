package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestContainerNameUsesFolderAndTwelveHexCharacterHash(t *testing.T) {
	for _, slot := range []string{"project", "profile-pi-basic", "profile-pi-basic.project"} {
		workspace := "/workspace/example"
		sum := sha256.Sum256([]byte(workspace + "\x00" + slot))
		want := "devbox-example-" + hex.EncodeToString(sum[:6]) + "." + strings.ReplaceAll(slot, ":", "-")
		if got := ContainerName(workspace, slot); got != want {
			t.Fatalf("name = %q, want %q", got, want)
		}
	}
	if ContainerName("/one/example", "project") == ContainerName("/two/example", "project") {
		t.Fatal("different workspace paths share a name")
	}
	if ContainerName("/workspace/example", "project") == ContainerName("/workspace/example", "profile-project") {
		t.Fatal("project and profile slots share a name")
	}
}

func TestContainerFolderIsSafeAndBounded(t *testing.T) {
	valid := regexp.MustCompile(`^devbox-[a-z0-9_.-]+-[a-f0-9]{12}\.project$`)
	for _, tc := range []struct{ folder, want string }{
		{"My App", "my-app"},
		{"my.app_v2", "my.app_v2"},
		{"--My App..", "my-app"},
		{"a ?\t\n b", "a-b"},
		{"日本語", "workspace"},
		{"...", "workspace"},
		{strings.Repeat("a", 80), strings.Repeat("a", 32)},
		{strings.Repeat("a", 31) + "-suffix", strings.Repeat("a", 31)},
	} {
		name := ContainerName("/parent/"+tc.folder, "project")
		if !strings.HasPrefix(name, "devbox-"+tc.want+"-") || !valid.MatchString(name) {
			t.Fatalf("folder %q produced %q, want folder %q", tc.folder, name, tc.want)
		}
	}
	if name := ContainerName("/", "project"); !strings.HasPrefix(name, "devbox-workspace-") || !valid.MatchString(name) {
		t.Fatal("root workspace produced an unsafe name", name)
	}
	for _, pair := range [][2]string{
		{"/parent/My App", "/parent/my-app"},
		{"/parent/" + strings.Repeat("a", 40), "/parent/" + strings.Repeat("a", 41)},
	} {
		if ContainerName(pair[0], "project") == ContainerName(pair[1], "project") {
			t.Fatal("sanitized/truncated folders lost full-path identity", pair)
		}
	}
}

func TestContainerNameUsesCanonicalFolderThroughSymlinks(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "Real Folder")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	actual, err := Identify(workspace, "pi-basic", false)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := Identify(alias, "pi-basic", false)
	if err != nil || actual != linked || !strings.HasPrefix(actual.Name, "devbox-real-folder-") {
		t.Fatal("symlink alias changed canonical identity", actual, linked, err)
	}
}
