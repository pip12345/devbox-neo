package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func requireText(t *testing.T, text string, expected ...string) {
	t.Helper()
	for _, part := range expected {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in %s", part, text)
		}
	}
}

func TestSchemaDiagnosticsIdentifyStructureWithoutValues(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		expected   []string
	}{
		{"unknown field", `{"version":1,"old_setting":"private-value"}`, []string{`unknown field "old_setting"`}},
		{"wrong array", `{"version":1,"extra_env":"private-value"}`, []string{`field "extra_env"`, "expected array, got string", "byte"}},
		{"array member", `{"version":1,"extra_env":[987654321]}`, []string{`field "extra_env"`, "expected string, got number"}},
		{"nested type", `{"version":1,"proxy":{"enabled":"private-value"}}`, []string{`field "proxy.enabled"`, "expected boolean, got string"}},
		{"overflow", `{"version":987654321987654321987654321}`, []string{`field "version"`, "expected integer, got number"}},
		{"syntax", `{"version":1,"extra_env":private-value}`, []string{"malformed JSON", "byte"}},
		{"truncated", `{"version":1,"extra_env":`, []string{"incomplete JSON document"}},
		{"duplicate", `{"version":1,"extra_env":["private-value"],"extra_env":[]}`, []string{"duplicate JSON field"}},
		{"root type", `["private-value"]`, []string{"expected a JSON object"}},
		{"trailing", `{"version":1} {"token":"private-value"}`, []string{"expected exactly one JSON value"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := convertLayer([]byte(tc.data))
			if err == nil {
				t.Fatal("invalid source accepted")
			}
			requireText(t, err.Error(), append([]string{"config.json"}, tc.expected...)...)
			for _, secret := range []string{"private-value", "987654321"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("diagnostic leaked value", err)
				}
			}
		})
	}
	if got := schemaDiagnostic(errors.New("private-value")); strings.Contains(got, "private-value") {
		t.Fatal("unknown error text was exposed")
	}
}

func TestVersionDiagnosticsDistinguishMissingNullZeroAndMismatch(t *testing.T) {
	for _, tc := range []struct{ data, found string }{
		{`{}`, "missing"}, {`{"version":null}`, "null"}, {`{"version":0}`, "0"}, {`{"version":3}`, "3"},
	} {
		_, _, err := convertLayer([]byte(tc.data))
		if err == nil {
			t.Fatal("unsupported layer accepted", tc.data)
		}
		requireText(t, err.Error(), `config.json: field "version"`, "found "+tc.found+"; expected version 1")
	}
}

func rewriteObject(t *testing.T, path string, change func(map[string]json.RawMessage)) {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(read(t, path)), &value); err != nil {
		t.Fatal(err)
	}
	change(value)
	put(t, path, string(encode(value)))
}

func TestSessionDiagnosticsSeparateVersionsAndCreationSettings(t *testing.T) {
	for _, creation := range []string{"missing", "null"} {
		t.Run(creation, func(t *testing.T) {
			p, name := fixture(t)
			root := filepath.Join(p.Source, "sessions", name)
			rewriteObject(t, filepath.Join(root, "session.json"), func(v map[string]json.RawMessage) { v["version"] = []byte("2") })
			rewriteObject(t, filepath.Join(root, "metadata.json"), func(v map[string]json.RawMessage) {
				v["metadata_version"] = []byte("3")
				delete(v, "ownership_version")
				if creation == "missing" {
					delete(v, "creation_settings")
				} else {
					v["creation_settings"] = []byte("null")
				}
			})
			v := inventory(t, p)
			text := strings.Join(v.item("session:"+name).Issues, "\n")
			requireText(t, text, `session.json: field "version": found 2; expected version 1`, `metadata.json: field "metadata_version": found 3; expected version 4`, `metadata.json: field "ownership_version": found missing; expected version 1`, "creation_settings is "+creation)
			if _, err := v.Select(Selection{}); err == nil {
				t.Fatal("unsupported source accepted")
			}
			absent(t, p.Work)
		})
	}
	p, name := fixture(t)
	rewriteObject(t, filepath.Join(p.Source, "sessions", name, "metadata.json"), func(v map[string]json.RawMessage) { delete(v, "creation_settings") })
	issues := inventory(t, p).item("session:" + name).Issues
	if len(issues) != 1 || !strings.Contains(issues[0], "creation_settings is missing") {
		t.Fatal(issues)
	}
}

func TestSessionDiagnosticsIdentifyReadAndDecodeFailuresSeparately(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	if err := os.Remove(filepath.Join(root, "session.json")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "metadata.json"), `{"metadata_version":"private-value"}`)
	text := strings.Join(inventory(t, p).item("session:"+name).Issues, "\n")
	requireText(t, text, "Cannot read session.json", "does not exist", "Invalid metadata.json", `field "metadata_version"`, "expected integer, got string")
	if strings.Contains(text, "private-value") {
		t.Fatal("metadata leaked")
	}
}

func TestDiagnosticValuesStayOutOfReportsAndJournals(t *testing.T) {
	p, name := fixture(t)
	put(t, filepath.Join(p.Source, "profiles/work/config.json"), `{"version":1,"extra_env":"private-value"}`)
	put(t, filepath.Join(p.Source, "sessions", name, "metadata.json"), `{"metadata_version":987654321987654321987654321}`)
	v := inventory(t, p)
	var out bytes.Buffer
	if err := Report(&out, v, nil); err != nil {
		t.Fatal(err)
	}
	j, err := Stage(t.Context(), v, Selection{Skip: []string{"profile:work", "session:" + name}}, &fakeSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{out.String(), read(t, filepath.Join(p.Work, "report.txt")), read(t, filepath.Join(p.Work, "journal.json"))} {
		for _, secret := range []string{"private-value", "987654321", "fixture-private-value"} {
			if strings.Contains(text, secret) {
				t.Fatal("private source value persisted in diagnostic")
			}
		}
		requireText(t, text, "extra_env", "expected array, got string", "metadata_version", "expected integer, got number")
	}
	if j.Phase != "prepared" {
		t.Fatal(j.Phase)
	}
}

func TestGlobalSchemaDiagnosticsNameTheFileAndField(t *testing.T) {
	p, _ := fixture(t)
	put(t, filepath.Join(p.Source, "global.json"), `{"version":2,"global_env":"private-value"}`)
	_, err := InventorySource(t.Context(), p)
	if err == nil {
		t.Fatal("invalid global accepted")
	}
	requireText(t, err.Error(), "global.json", `field "global_env"`, "expected array, got string")
	if strings.Contains(err.Error(), "private-value") {
		t.Fatal("global value leaked")
	}
	put(t, filepath.Join(p.Source, "global.json"), `{"version":1}`)
	_, err = InventorySource(t.Context(), p)
	if err == nil {
		t.Fatal("unsupported global accepted")
	}
	requireText(t, err.Error(), `global.json: field "version": found 1; expected version 2`)
	absent(t, p.Work)
}

func TestWorkspaceDiagnosticsDistinguishFilesystemFailures(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	put(t, file, "private-workspace-content")
	for name, target := range map[string]string{"alias": directory, "dangling": filepath.Join(root, "missing"), "loop": "loop"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path     string
		expected []string
	}{
		{filepath.Join(root, "missing"), []string{"Cannot resolve workspace", "does not exist"}},
		{filepath.Join(root, "dangling"), []string{"symlink target does not exist"}},
		{file, []string{"Workspace is not a directory"}},
		{filepath.Join(file, "child"), []string{"path component is not a directory"}},
		{filepath.Join(root, "alias"), []string{"not canonical", "resolves to", directory}},
		{filepath.Join(root, "loop"), []string{"too many symbolic links"}},
	} {
		requireText(t, workspaceDiagnostic(tc.path), tc.expected...)
	}
	if problem := workspaceDiagnostic(directory); problem != "" {
		t.Fatal(problem)
	}
	for _, err := range []error{os.ErrPermission, &os.PathError{Op: "stat", Path: "public-path", Err: syscall.EACCES}} {
		if got := filesystemDiagnostic(err); got != "permission denied" {
			t.Fatal(got)
		}
	}
}

func TestWorkspaceDiagnosticAppearsInInventory(t *testing.T) {
	p, name := fixture(t)
	workspace := filepath.Join(filepath.Dir(p.Source), "api")
	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	requireText(t, strings.Join(v.item("session:"+name).Issues, "\n"), "Cannot resolve workspace", workspace, "does not exist", "explicitly skip")
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("missing workspace accepted")
	}
}

func TestGeneratedLinkFailureShowsTargetAndAttemptedHostMapping(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			p, name := fixture(t)
			root := filepath.Join(p.Source, "sessions", name)
			link := filepath.Join(root, "pi/extensions-disabled")
			target := "/devbox/harness-config/extensions"
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if rejected {
				if err := os.MkdirAll(filepath.Join(root, ".staged-harness"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(root, ".staged-harness/pi")); err != nil {
					t.Fatal(err)
				}
			}
			v := inventory(t, p)
			_, err := Stage(t.Context(), v, Selection{}, &fakeSource{}, nil)
			if err == nil {
				t.Fatal("unresolved generated link accepted")
			}
			text := err.Error()
			requireText(t, text, link, target, filepath.Join(root, ".staged-harness/pi/extensions"))
			if rejected {
				requireText(t, text, "rejected", "symlink component")
			} else {
				requireText(t, text, "unavailable", "does not exist")
			}
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Fatal("source link changed")
			}
			absent(t, p.Work)
		})
	}
}

func TestUnmappedLayoutDiagnosticShowsEntryKindsAndRootLink(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	for _, dir := range []string{".internal", "harnesses"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(filepath.Join(root, "pi"), filepath.Join(root, "harnesses/pi")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("harnesses/pi", filepath.Join(root, "pi")); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	text := strings.Join(v.item("session:"+name).Issues, "\n")
	requireText(t, text, `".internal" (directory)`, `"harnesses" (directory)`, `-> "harnesses/pi"`, "symlink-backed harness roots are not supported")
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("unmapped layout accepted")
	}
}

func TestInventoriedDoesNotClaimImportReadiness(t *testing.T) {
	p, _ := fixture(t)
	v := inventory(t, p)
	var out bytes.Buffer
	if err := Report(&out, v, nil); err != nil {
		t.Fatal(err)
	}
	requireText(t, out.String(), "[Inventoried] profile:work", "not a validated import", "Docker checks and final configuration review")
	if strings.Contains(out.String(), "[Ready]") {
		t.Fatal("inventory claims readiness")
	}
}
