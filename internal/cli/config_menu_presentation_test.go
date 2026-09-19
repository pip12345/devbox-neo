package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/resource"
)

func TestEmptyListShowsOnlyAddAndBack(t *testing.T) {
	field := resource.ConfigField{Key: "mounts", Kind: "list"}
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("0\n")), out: &out}
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	if err := editList(m, s, owner, field); err != nil {
		t.Fatal(err)
	}
	want := "\nNo mounts configured here.\n\nWhat would you like to do?\n   [1]  Add mount\n\n   [0]  Back\n\n   Choose a number > "
	if out.String() != want {
		t.Fatalf("empty list should be a simple add/back menu:\n%s", out.String())
	}
}

func TestListOperationsSaveWithoutApprovalSteps(t *testing.T) {
	field := resource.ConfigField{Key: "mounts", Kind: "list"}
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("1\n/data:/data:ro\n0\n")), out: &out}
	if err := editList(m, s, owner, field); err != nil {
		t.Fatal(err)
	}
	source, _ := s.ConfigSource(owner)
	entries, err := configEntries(source[field.Key], field)
	if err != nil || len(entries) != 1 || entries[0] != "/data:/data:ro" {
		t.Fatal("back lost the completed add", entries, err)
	}
	for _, text := range []string{"Edit mount", "Remove mount", "Saved Mounts", "Reset to inherited"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q after adding a mount:\n%s", text, out.String())
		}
	}
	for _, text := range []string{"Use this list", "Clear local entries", "Save changes", "Discard changes", "[1] Yes", "[2] No"} {
		if strings.Contains(out.String(), text) {
			t.Fatalf("extra approval step %q:\n%s", text, out.String())
		}
	}
}

func TestMountEditorSavesOperationsAndCancelsOnlyIncompleteInput(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	n := fieldNumber(t, "profile", "mounts")
	out, err := runMenu(t, s, owner, n+"\n1\n/data:/data:ro\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	if strings.Contains(out, "Edit local value") || strings.Contains(out, "Use this list") {
		t.Fatal("list editor still uses the redundant submenu", out)
	}
	values, _ := s.ConfigSource(owner)
	entries, err := configEntries(values["mounts"], resource.ConfigField{Kind: "list"})
	if err != nil || len(entries) != 1 || entries[0] != "/data:/data:ro" {
		t.Fatal("mount was not saved", entries, err)
	}
	// Reset immediately removes the source key, rather than saving an empty list.
	out, err = runMenu(t, s, owner, n+"\n4\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	values, _ = s.ConfigSource(owner)
	if _, exists := values["mounts"]; exists {
		t.Fatal("reset did not remove the source key")
	}
	before, _ := os.ReadFile(filepath.Join(owner.Root, "config.json"))
	for _, input := range []string{n + "\n1\n:back\n0\n0\n", n + "\n1\n", n + "\n1\n/data:/data:ro"} {
		if out, err = runMenu(t, s, owner, input); err != nil {
			t.Fatal(out, err)
		}
		after, _ := os.ReadFile(filepath.Join(owner.Root, "config.json"))
		if !bytes.Equal(before, after) {
			t.Fatal("canceling incomplete input saved a list")
		}
	}
	out, err = runMenu(t, s, owner, n+"\n1\n/data:/data:ro\n1\n/unfinished")
	if err != nil {
		t.Fatal(out, err)
	}
	values, _ = s.ConfigSource(owner)
	entries, err = configEntries(values["mounts"], resource.ConfigField{Kind: "list"})
	if err != nil || len(entries) != 1 || entries[0] != "/data:/data:ro" {
		t.Fatal("EOF lost a completed operation or saved unfinished input", entries, err)
	}
}

func TestSettingsOverviewUsesReadableValues(t *testing.T) {
	for _, tt := range []struct {
		field     resource.ConfigField
		raw, want string
	}{
		{resource.ConfigField{Kind: "list"}, "null", "value: None"},
		{resource.ConfigField{Kind: "list"}, "[]", "value: None"},
		{resource.ConfigField{Kind: "extensions"}, "{}", "value: None"},
		{resource.ConfigField{Kind: "string"}, `"pi"`, "value: pi"},
		{resource.ConfigField{Kind: "list"}, `["bash"]`, "value: - bash"},
		{resource.ConfigField{Kind: "list"}, `[""]`, "value: - (empty)"},
		{resource.ConfigField{Kind: "bool"}, "false", "value: No"},
		{resource.ConfigField{Kind: "list", Sensitive: true}, `["TOKEN=private-value"]`, "value: - TOKEN=<redacted>"},
		{resource.ConfigField{Kind: "string"}, `"\u001b[31m"`, `value: "\x1b[31m"`},
	} {
		var out bytes.Buffer
		row := configDisplayRow{label: "value", value: menuConfigValue(json.RawMessage(tt.raw), tt.field)}
		if err := printConfigRows(&out, []configDisplayRow{row}, "  ", 80); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(strings.Fields(out.String()), " "); got != tt.want {
			t.Fatalf("%s displayed as %q, want %q", tt.raw, got, tt.want)
		}
	}
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	s.Init(context.Background(), owner, resource.InitOptions{Harness: "pi"})
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Harness pi profile", "Shell command bash default", "Mounts None default", "VS Code extensions None default"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), text) {
			t.Fatalf("missing readable summary %q:\n%s", text, out)
		}
	}
	originColumn := -1
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "   [") || strings.Contains(line, "[0]") {
			continue
		}
		parts := strings.Fields(line)
		column := strings.LastIndex(line, parts[len(parts)-1])
		if originColumn >= 0 && column != originColumn {
			t.Fatalf("origin labels are not aligned:\n%s", out)
		}
		originColumn = column
	}
	if originColumn < 0 || !strings.Contains(out, "\n\n   [0]  Done\n\n   Choose a number > ") {
		t.Fatalf("missing aligned origins or menu spacing:\n%s", out)
	}
	if !strings.Contains(out, "Profile · basic") || !strings.Contains(out, "Setting") || !strings.Contains(out, "Value") || !strings.Contains(out, "Source") {
		t.Fatal("missing title or column headings", out)
	}
	for _, text := range []string{"local:", "effective:", "null", "[]", "{}", "Changes save immediately", "Creation changes require", "Values include inherited", "Select a setting", s.Home} {
		if strings.Contains(out, text) {
			t.Fatalf("raw storage detail %q in the overview:\n%s", text, out)
		}
	}
}
