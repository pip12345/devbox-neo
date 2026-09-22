package resource

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/harness"
)

func TestArtifactSetupLeavesDevboxSkillInheritedAndOverridable(t *testing.T) {
	for _, name := range []string{"pi", "opencode"} {
		for _, form := range []string{"named", "path"} {
			t.Run(name+"/"+form, func(t *testing.T) {
				s := fixture(t)
				ctx := context.Background()
				input := "basic"
				if form == "path" {
					input = filepath.Join(t.TempDir(), "config")
				}
				owner, err := s.ConfigDirectory(input, t.TempDir(), t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.CreateConfig(ctx, owner, SetupOptions{}); err != nil {
					t.Fatal(err)
				}
				options := SetupOptions{ArtifactHarness: name, Artifacts: []string{"harness-config"}}
				result, err := s.EditConfig(ctx, owner, options)
				if err != nil || len(result.Created) != 1 {
					t.Fatalf("expected only the harness settings file: %+v, %v", result, err)
				}
				const skill = "skills/devbox/SKILL.md"
				source := filepath.Join(owner.Root, name, skill)
				if _, err = os.Stat(source); !os.IsNotExist(err) {
					t.Fatalf("setup populated inherited Devbox guidance: %v", err)
				}
				h, err := harness.Load(s.Home, name)
				if err != nil {
					t.Fatal(err)
				}
				checkTree := func(want string) {
					t.Helper()
					r, err := artifact.Resolve([]config.Source{{Label: owner.Name, Path: owner.Root}}, config.Host{})
					if err != nil {
						t.Fatal(err)
					}
					files, _, err := r.Tree(h)
					if err != nil || string(files[skill].Data) != want {
						t.Fatal("wrong resolved skill", err)
					}
				}
				builtin := string(h.Defaults[skill].Data)
				if builtin == "" {
					t.Fatal("missing built-in Devbox skill")
				}
				checkTree(builtin)
				const override = "User-supplied Devbox guidance\n"
				put(t, source, override)
				if _, err = s.EditConfig(ctx, owner, options); err != nil {
					t.Fatal(err)
				}
				if string(get(t, source)) != override {
					t.Fatal("setup changed an explicit skill override")
				}
				checkTree(override)
			})
		}
	}
}
