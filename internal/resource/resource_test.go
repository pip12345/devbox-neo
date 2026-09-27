package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func fixture(t *testing.T) Service {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Service{Home: s.Home}
}
func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func get(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConfigHintsKeepEnteredReference(t *testing.T) {
	s := fixture(t)
	owner, err := s.ConfigDirectory("./local-config", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EditConfig(context.Background(), owner, SetupOptions{Artifacts: []string{"setup.sh"}})
	var actionable *commanderror.Error
	if !errors.As(err, &actionable) || !reflect.DeepEqual(actionable.Next[0].Command, []string{"devbox-neo", "config", "create", "./local-config"}) {
		t.Fatal(err)
	}
	if _, err := s.CreateConfig(context.Background(), owner, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckConfigCreation(owner); !errors.As(err, &actionable) || !reflect.DeepEqual(actionable.Next[0].Command, []string{"devbox-neo", "config", "edit", "./local-config"}) {
		t.Fatal(err)
	}
}

func TestConfigOwnersCanonicalizeAliases(t *testing.T) {
	s := fixture(t)
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	first, err := s.ConfigDirectory(filepath.Join(root, "new"), root, root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ConfigDirectory(filepath.Join(alias, "new"), root, root)
	if err != nil || first.Root != second.Root {
		t.Fatal("aliases do not share a config owner", first, second, err)
	}
	if _, err := s.CreateConfig(context.Background(), first, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfig(context.Background(), second, SetupOptions{}); err == nil {
		t.Fatal("alias bypassed existing-config rejection")
	}
}

func TestArtifactSetupKeepsDockerfilesAndExecutableIntent(t *testing.T) {
	s, owner := configDirectoryFixture(t)
	ctx := context.Background()
	if _, err := s.CreateConfig(ctx, owner, SetupOptions{Artifacts: []string{"docker/Dockerfile", "setup.sh", "before-open.sh"}}); err != nil {
		t.Fatal(err)
	}
	for file, mode := range map[string]os.FileMode{"config.json": 0600, "docker/Dockerfile": 0600, "setup.sh": 0700, "before-open.sh": 0700} {
		info, err := os.Stat(filepath.Join(owner.Root, file))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal(file, info, err)
		}
	}
	put(t, filepath.Join(owner.Root, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nCOPY asset /opt/asset\n")
	if err := os.Chmod(filepath.Join(owner.Root, "setup.sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditConfig(ctx, owner, SetupOptions{Artifacts: []string{"docker/Dockerfile", "setup.sh"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(get(t, filepath.Join(owner.Root, "docker/Dockerfile"))), "COPY asset") {
		t.Fatal("setup replaced a Dockerfile")
	}
	info, err := os.Stat(filepath.Join(owner.Root, "setup.sh"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("setup changed existing script permissions", err)
	}
	if err := fsutil.WriteNew(filepath.Join(owner.Root, "setup.sh"), []byte("overwrite"), 0600); !os.IsExist(err) {
		t.Fatal("no-replace publication replaced an existing script", err)
	}
}
