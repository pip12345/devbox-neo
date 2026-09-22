package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigReferencePathsAndPortability(t *testing.T) {
	const home, cwd, workspace, userHome = "/selected-home", "/work", "/work/api", "/users/dev"
	for _, tc := range []struct {
		input, kind, saved, absolute string
	}{
		{"base", ReferenceFixed, "/selected-home/configs/base", "/selected-home/configs/base"},
		{"./api/devconfig", ReferenceRelative, "devconfig", "/work/api/devconfig"},
		{"configs/local", ReferenceRelative, "../configs/local", "/work/configs/local"},
		{"../shared", ReferenceRelative, "../../shared", "/shared"},
		{"~/coolconfig", ReferenceFixed, "/users/dev/coolconfig", "/users/dev/coolconfig"},
		{"/work/api/devconfig/", ReferenceFixed, "/work/api/devconfig", "/work/api/devconfig"},
		{".", ReferenceRelative, "..", "/work"},
		{"..", ReferenceRelative, "../..", "/"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			r, err := CaptureReference(home, workspace, cwd, userHome, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if r.Label != tc.input || r.Kind != tc.kind || r.Path != tc.saved {
				t.Fatalf("captured %+v", r)
			}
			source, err := r.Expand(workspace)
			if err != nil || source.Path != tc.absolute {
				t.Fatalf("expanded %+v: %v", source, err)
			}
			moved, err := r.Expand("/elsewhere/api")
			if err != nil {
				t.Fatal(err)
			}
			want := tc.absolute
			if tc.kind == ReferenceRelative {
				want = filepath.Join("/elsewhere/api", tc.saved)
			}
			if moved.Path != want {
				t.Fatalf("moved to %q, want %q", moved.Path, want)
			}
		})
	}
}

func TestRelativeReferenceFromSymlinkedCWDRebasesWithWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "real", "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ input, saved string }{
		{"./.devbox", ".devbox"},
		{"./missing/config", "missing/config"},
		{"./linked", "linked"},
	} {
		r, err := CaptureReference(filepath.Join(root, "home"), workspace, alias, root, tc.input)
		if err != nil || r.Kind != ReferenceRelative || r.Path != tc.saved {
			t.Fatalf("capture %q: %+v, %v", tc.input, r, err)
		}
		moved, err := r.Expand(filepath.Join(root, "destination"))
		if err != nil || moved.Path != filepath.Join(root, "destination", tc.saved) {
			t.Fatalf("rebase %q: %+v, %v", tc.input, moved, err)
		}
	}
}

func TestConfigReferenceUsesSelectedHome(t *testing.T) {
	for _, home := range []string{"/users/dev/.devbox-neo", "/custom/installation"} {
		path, err := ConfigPath(home, "/cwd", "/users/dev", "base")
		if err != nil || path != filepath.Join(home, "configs/base") {
			t.Fatalf("bare reference: %q %v", path, err)
		}
		path, err = ConfigPath(home, "/cwd", "/users/dev", "./base")
		if err != nil || path != "/cwd/base" {
			t.Fatalf("filesystem reference: %q %v", path, err)
		}
	}
}

func TestConfigReferenceValidationDoesNotReadSources(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "missing-workspace")
	r := Reference{Label: "missing", Kind: ReferenceRelative, Path: "../missing-config"}
	if _, err := r.Expand(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveReferences(workspace, []Reference{r}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime resolution should expose missing source: %v", err)
	}
	for _, bad := range []Reference{
		{Label: "x", Kind: ReferenceFixed, Path: "relative"},
		{Label: "x", Kind: ReferenceRelative, Path: "/absolute"},
		{Label: "x", Kind: "unknown", Path: "."},
		{Label: "x", Kind: ReferenceRelative, Path: "a/../b"},
		{Label: "", Kind: ReferenceRelative, Path: "."},
		{Label: "x", Kind: ReferenceRelative, Path: ""},
		{Label: "x", Kind: ReferenceFixed, Path: "/bad\x00path"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if _, err := CaptureReference("/home", "relative-workspace", "/cwd", "/user", "base"); err == nil {
		t.Fatal("accepted noncanonical workspace")
	}
	if _, err := ConfigPath("/home", "/cwd", "/user", ""); err == nil {
		t.Fatal("accepted empty reference")
	}
	if _, err := ResolveReferences(workspace, nil); err == nil {
		t.Fatal("accepted an empty runtime source chain")
	}
}

func TestResolveReferencesRejectsAliasesAndKeepsOrder(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"first", "second"} {
		if err := os.Mkdir(filepath.Join(workspace, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(workspace, "first"), filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	first := Reference{Label: "first", Kind: ReferenceRelative, Path: "first"}
	second := Reference{Label: "second", Kind: ReferenceRelative, Path: "second"}
	before := []Reference{second, first}
	sources, err := ResolveReferences(workspace, before)
	if err != nil || len(sources) != 2 || sources[0].Label != "second" || sources[1].Label != "first" {
		t.Fatalf("source order: %+v, %v", sources, err)
	}
	if !reflect.DeepEqual(before, []Reference{second, first}) {
		t.Fatal("resolution mutated saved references")
	}
	for _, duplicate := range []Reference{
		first,
		{Label: "absolute", Kind: ReferenceFixed, Path: filepath.Join(workspace, "first")},
		{Label: "alias", Kind: ReferenceRelative, Path: "alias"},
	} {
		if _, err := ResolveReferences(workspace, []Reference{first, duplicate}); err == nil {
			t.Fatalf("accepted duplicate directory: %+v", duplicate)
		}
	}
	file := filepath.Join(workspace, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveReferences(workspace, []Reference{{Label: "file", Kind: ReferenceFixed, Path: file}}); err == nil {
		t.Fatal("accepted file as a config directory")
	}
}
