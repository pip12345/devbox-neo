package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestSourceCandidatesDeduplicateByCanonicalDirectoryInGroupOrder(t *testing.T) {
	s := menuService(t)
	workspace, cwd := t.TempDir(), t.TempDir()
	completionFile(t, s.Home, "configs/base/config.json", `{"version":1}`)
	completionFile(t, workspace, "local/config.json", `{"version":1}`)
	completionFile(t, cwd, "local/config.json", `{"version":1}`)
	named := filepath.Join(s.Home, "configs/base")
	for link, target := range map[string]string{
		filepath.Join(s.Home, "configs/z-alias"): named,
		filepath.Join(workspace, "named-alias"):  named,
		filepath.Join(workspace, "z-alias"):      filepath.Join(workspace, "local"),
		filepath.Join(cwd, "named-alias"):        named,
		filepath.Join(cwd, "workspace-alias"):    filepath.Join(workspace, "local"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("1\n"), &out), home: s.Home, workspace: workspace, cwd: cwd, userHome: t.TempDir()}
	candidates, err := p.candidates()
	if err != nil {
		t.Fatal(err)
	}
	var got [][3]string
	for _, c := range candidates {
		got = append(got, [3]string{c.Name, c.group, c.directory})
	}
	want := [][3]string{
		{"base", "Named configs", cwd},
		{"./local", "Workspace configs", workspace},
		{"./local", "Current-directory configs", cwd},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("wrong canonical deduplication or group precedence", got)
	}
	ref, chosen, err := p.choose(nil, "Back")
	if err != nil || !chosen || ref.Kind != config.ReferenceFixed || ref.Path != named {
		t.Fatal("deduplication lost named-config reference semantics", ref, chosen, err, out.String())
	}
}

func TestSourceCandidatesShareWorkspaceAndInvokingDirectory(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(map[bool]string{false: "same path", true: "symlink alias"}[alias], func(t *testing.T) {
			s := menuService(t)
			workspace := t.TempDir()
			completionFile(t, workspace, "config.json", `{"version":1}`)
			completionFile(t, workspace, "child/config.json", `{"version":1}`)
			cwd := workspace
			if alias {
				cwd = filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(workspace, cwd); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("0\n"), &out), home: s.Home, workspace: workspace, cwd: cwd, userHome: t.TempDir()}
			candidates, err := p.candidates()
			if err != nil || len(candidates) != 2 {
				t.Fatal(candidates, err)
			}
			for _, c := range candidates {
				if c.group != "Workspace configs" {
					t.Fatal("same directory appeared as a second group", c)
				}
			}
			if _, chosen, err := p.choose(nil, "Back"); err != nil || chosen {
				t.Fatal("discovery selected a config on cancellation", chosen, err)
			}
			if strings.Contains(out.String(), "Current-directory configs") {
				t.Fatal("duplicate directory group shown", out.String())
			}
		})
	}
}

func TestSourceCandidatesDeduplicateOverlappingDiscoveryRoots(t *testing.T) {
	s := menuService(t)
	cwd := t.TempDir()
	workspace := filepath.Join(cwd, "workspace")
	completionFile(t, workspace, "config.json", `{"version":1}`)
	var out bytes.Buffer
	p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("1\n"), &out), home: s.Home, workspace: workspace, cwd: cwd, userHome: t.TempDir()}
	candidates, err := p.candidates()
	if err != nil || len(candidates) != 1 || candidates[0].Name != "." || candidates[0].group != "Workspace configs" {
		t.Fatal("workspace root was repeated as a cwd child", candidates, err)
	}
	ref, chosen, err := p.choose(nil, "Back")
	if err != nil || !chosen || ref.Kind != config.ReferenceRelative || ref.Path != "." {
		t.Fatal("workspace root suggestion resolved against cwd", ref, chosen, err)
	}
}

func TestSourceCandidatesDiscoveryFailureKeepsOtherGroups(t *testing.T) {
	for _, missing := range []string{"workspace", "cwd"} {
		t.Run(missing, func(t *testing.T) {
			s := menuService(t)
			workspace, cwd := t.TempDir(), t.TempDir()
			completionFile(t, s.Home, "configs/base/config.json", `{"version":1}`)
			completionFile(t, workspace, "local/config.json", `{"version":1}`)
			completionFile(t, cwd, "local/config.json", `{"version":1}`)
			group := "Workspace configs"
			if missing == "workspace" {
				workspace = filepath.Join(workspace, "missing")
			} else {
				cwd = filepath.Join(cwd, "missing")
				group = "Current-directory configs"
			}
			var out bytes.Buffer
			p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("0\n"), &out), home: s.Home, workspace: workspace, cwd: cwd, userHome: t.TempDir()}
			candidates, err := p.candidates()
			if err != nil || len(candidates) != 2 || candidates[0].group != "Named configs" || candidates[1].group == group {
				t.Fatal("discovery failure hid unrelated candidates", candidates, err)
			}
			if _, chosen, err := p.choose(nil, "Back"); err != nil || chosen {
				t.Fatal("discovery failure prevented cancellation", chosen, err)
			}
			if !strings.Contains(out.String(), group+" discovery unavailable") {
				t.Fatal("discovery failure was silent", out.String())
			}
		})
	}
}
