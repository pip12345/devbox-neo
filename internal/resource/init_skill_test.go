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

func TestInitLeavesDevboxSkillInheritedAndOverridable(t *testing.T) {
	for _, name := range []string{"pi", "opencode"} {
		for _, kind := range []string{"profile", "project"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				s := fixture(t)
				ctx := context.Background()
				workspace := t.TempDir()
				var owner Owner
				var err error
				profile := ""
				if kind == "profile" {
					profile = "basic"
					owner, err = s.Profile(profile)
				} else {
					owner, err = s.Project(workspace)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Create(ctx, owner, ""); err != nil {
					t.Fatal(err)
				}
				options := InitOptions{Harness: name, Artifacts: []string{"harness-config"}}
				result, err := s.Init(ctx, owner, options)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Created) != 1 {
					t.Fatalf("expected only the harness settings file, got %v", result.Created)
				}
				const skill = "skills/devbox/SKILL.md"
				source := filepath.Join(owner.Root, name, skill)
				if _, err = os.Stat(source); !os.IsNotExist(err) {
					t.Fatalf("init populated the Devbox skill in user configuration: %v", err)
				}
				h, err := harness.Load(s.Home, name)
				if err != nil {
					t.Fatal(err)
				}
				checkTree := func(want string) {
					t.Helper()
					r, err := artifact.Resolve(s.Home, workspace, profile, config.Layer{})
					if err != nil {
						t.Fatal(err)
					}
					files, _, err := r.Tree(h)
					if err != nil {
						t.Fatal(err)
					}
					if got := string(files[skill].Data); got != want {
						t.Fatalf("wrong resolved skill: got %q, want %q", got, want)
					}
				}
				builtin := string(h.Defaults[skill].Data)
				if builtin == "" {
					t.Fatal("missing built-in Devbox skill")
				}
				checkTree(builtin)

				const override = "User-supplied Devbox guidance\n"
				put(t, source, override)
				if _, err = s.Init(ctx, owner, options); err != nil {
					t.Fatal(err)
				}
				if got := string(get(t, source)); got != override {
					t.Fatal("init changed an explicit skill override")
				}
				checkTree(override)
			})
		}
	}
}
