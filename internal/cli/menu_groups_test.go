package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowMenusShowOrderedGroups(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups []string
		run    func(*frontend, string) error
	}{
		{"recreate", []string{"Inspect", "Options", "Apply"}, func(f *frontend, target string) error { return f.recreate(target) }},
		{"transfer", []string{"Destination", "Preview", "Transfer"}, func(f *frontend, target string) error { _, err := f.transfer(target); return err }},
		{"single deletion", []string{"Options", "Delete"}, func(f *frontend, target string) error { return f.delete([]string{target}) }},
		{"bulk deletion", []string{"Targets", "Options", "Delete"}, func(f *frontend, _ string) error { return f.delete(nil) }},
		{"selected configs", []string{"Configs", "Inspect"}, func(f *frontend, target string) error {
			record, err := f.e.Locate(f.m.Context, target, "")
			if err != nil {
				return err
			}
			_, err = sourceChainMenu(f.m, f.e, record, "Back")
			return err
		}},
		{"config editor", []string{"Settings", "Files", "Manage"}, func(f *frontend, _ string) error { return f.editConfig("base") }},
		{"session browser", []string{"Create", "Navigate", "Manage"}, func(f *frontend, _ string) error { return f.browse(false) }},
		{"config browser", []string{"Create", "Navigate", "Manage"}, func(f *frontend, _ string) error { return f.browse(true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, out, _, target := frontendFixture(t, strings.NewReader("0\n"))
			if err := tc.run(f, target); err != nil {
				t.Fatal(err, out.String())
			}
			text := out.String()
			remaining := text
			for _, group := range tc.groups {
				header := "\n" + group + "\n"
				index := strings.Index(remaining, header)
				if index < 0 || strings.Count(text, header) != 1 {
					t.Fatal("missing, repeated, or out-of-order group", group, text)
				}
				remaining = remaining[index+len(header):]
			}
		})
	}
}

func TestUnreadableConfigMenuShowsOnlyManageGroup(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("0\n"))
	if err := os.WriteFile(filepath.Join(f.s.Home, "configs", "base", "config.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.editConfig("base"); err != nil {
		t.Fatal(err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "\nManage\n"+menuPrefix(1)+"Delete config") || strings.Contains(text, "\nSettings\n") || strings.Contains(text, "\nFiles\n") {
		t.Fatal("unavailable config actions left empty groups or changed numbering", text)
	}
}
