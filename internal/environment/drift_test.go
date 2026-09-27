package environment

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/harness"
)

func driftFixture(t *testing.T) (Spec, Request) {
	t.Helper()
	home, workspace := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "profiles/test")
	putBuild(t, filepath.Join(root, "config.json"), `{"version":1,"harness":"pi","env":["TOKEN=old-secret"],"docker_args":["--env=RAW=raw-secret"],"ports":["8080:80","9090:90"]}`)
	putBuild(t, filepath.Join(root, "docker/Dockerfile"), "FROM debian:bookworm-slim\n")
	putBuild(t, filepath.Join(root, "docker/.dockerignore"), "ignored\n")
	putBuild(t, filepath.Join(root, "docker/data"), "old data")
	putBuild(t, filepath.Join(root, "pi/custom.json"), `{"private":"file-secret"}`)
	putBuild(t, filepath.Join(root, "setup.sh"), "echo setup")
	putBuild(t, filepath.Join(root, "before-open.sh"), "echo entrypoint")
	q := Request{Home: home, Workspace: workspace, LocalName: "test", Sources: []config.Reference{{Label: "base", Kind: config.ReferenceFixed, Path: root}}, UID: 1000, GID: 1000, Salt: "test-installation"}
	s, err := Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Inputs.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, q
}

func cloneInputs(t *testing.T, inputs Inputs) Inputs {
	t.Helper()
	b, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	var result Inputs
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func hasInputChange(report Report, field, code string) bool {
	for _, inputChange := range report.PendingInputChanges {
		if inputChange.Field == field && (code == "" || inputChange.Code == code) {
			return true
		}
	}
	return false
}

func TestDetailedComparisonCoversFingerprintInputs(t *testing.T) {
	s, _ := driftFixture(t)
	newHash := Fingerprint("test-installation", "new input")
	tests := []struct {
		name, field, code string
		change            Change
		edit              func(*Inputs)
	}{
		{"base image", "base_image", "value_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.BaseImage = "ubuntu:24.04" }},
		{"harness", "harness", "value_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Harness = "opencode" }},
		{"definition", "harness_definition", "file_content_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Definition.Hash = newHash }},
		{"Dockerfile", "dockerfile", "file_content_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Stages[0].Dockerfile.Hash = newHash }},
		{"ignore rules", "ignore_rules", "file_content_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Stages[0].Ignore.Hash = newHash }},
		{"context content", "build_context", "file_content_changed", RebuildAndRecreate, func(i *Inputs) {
			f := i.Image.Stages[0].Context["data"]
			f.Hash = newHash
			i.Image.Stages[0].Context["data"] = f
		}},
		{"context permissions", "build_context", "file_mode_changed", RebuildAndRecreate, func(i *Inputs) {
			f := i.Image.Stages[0].Context["data"]
			f.Mode = 0755
			i.Image.Stages[0].Context["data"] = f
		}},
		{"context kind", "build_context", "file_kind_changed", RebuildAndRecreate, func(i *Inputs) {
			f := i.Image.Stages[0].Context["data"]
			f.Directory = true
			i.Image.Stages[0].Context["data"] = f
		}},
		{"context added", "build_context", "file_added", RebuildAndRecreate, func(i *Inputs) {
			f := i.Image.Stages[0].Context["data"]
			f.Source += "-new"
			i.Image.Stages[0].Context["new"] = f
		}},
		{"context removed", "build_context", "file_removed", RebuildAndRecreate, func(i *Inputs) { delete(i.Image.Stages[0].Context, "data") }},
		{"generated image layer", "generated_layer", "input_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Layer = newHash }},
		{"build arguments", "build_argument", "value_changed", RebuildAndRecreate, func(i *Inputs) { i.Image.Arguments["DEVBOX_UID"] = "1001" }},
		{"workspace", "workspace", "value_changed", Recreate, func(i *Inputs) { i.Container.Workspace += "-new" }},
		{"network", "network", "value_changed", Recreate, func(i *Inputs) { i.Container.Network = "host" }},
		{"stores", "harness_stores", "entry_added", Recreate, func(i *Inputs) {
			i.Container.Stores = append(i.Container.Stores, harness.Store{Name: "new", Target: "/home/devuser/new", Scope: "environment"})
		}},
		{"auth mounts", "auth_mounts", "entry_removed", Recreate, func(i *Inputs) { i.Container.Auth = nil }},
		{"env changed", "env", "value_changed", Recreate, func(i *Inputs) { i.Container.Env["TOKEN"] = newHash }},
		{"env added", "env", "entry_added", Recreate, func(i *Inputs) { i.Container.Env["NEW_TOKEN"] = newHash }},
		{"env removed", "env", "entry_removed", Recreate, func(i *Inputs) { delete(i.Container.Env, "TOKEN") }},
		{"setup", "setup", "file_content_changed", Recreate, func(i *Inputs) { i.Container.Setup[0].Hash = newHash }},
		{"setup removed", "setup", "file_removed", Recreate, func(i *Inputs) { i.Container.Setup = nil }},
		{"mounts", "mounts", "entry_added", Recreate, func(i *Inputs) {
			i.Container.Mounts = []docker.Mount{{Source: "/public/source", Target: "/public/target", ReadOnly: true}}
		}},
		{"ports", "ports", "entry_added", Recreate, func(i *Inputs) { i.Container.Ports = append(i.Container.Ports, "8000:8000") }},
		{"port ordering", "ports", "order_changed", Recreate, func(i *Inputs) {
			i.Container.Ports[0], i.Container.Ports[1] = i.Container.Ports[1], i.Container.Ports[0]
		}},
		{"Docker args", "docker_args", "entry_added", Recreate, func(i *Inputs) { i.Container.RawArgs = append(i.Container.RawArgs, "--privileged") }},
		{"shadowed raw env", "docker_env", "input_changed", Recreate, func(i *Inputs) { i.Container.RawArgsHash = newHash }},
		{"metadata", "metadata", "input_changed", Recreate, func(i *Inputs) { i.Container.Metadata = "[]" }},
		{"host alias", "host_alias", "value_changed", Recreate, func(i *Inputs) { i.Container.HostAlias = "other.host" }},
		{"runtime assets", "runtime_assets", "input_changed", RuntimeSync, func(i *Inputs) { i.Runtime.Assets = newHash }},
		{"config content", "managed_config", "file_content_changed", RuntimeSync, func(i *Inputs) {
			f := i.Runtime.Files["custom.json"]
			f.Hash = newHash
			i.Runtime.Files["custom.json"] = f
		}},
		{"config executable", "managed_config", "file_mode_changed", RuntimeSync, func(i *Inputs) {
			f := i.Runtime.Files["custom.json"]
			f.Mode = 0100
			i.Runtime.Files["custom.json"] = f
		}},
		{"config removed", "managed_config", "file_removed", RuntimeSync, func(i *Inputs) { delete(i.Runtime.Files, "custom.json") }},
		{"before open", "before_open", "file_content_changed", RuntimeSync, func(i *Inputs) { i.Runtime.BeforeOpen[0].Hash = newHash }},
		{"before open removed", "before_open", "file_removed", RuntimeSync, func(i *Inputs) { i.Runtime.BeforeOpen = nil }},
		{"launch args", "launch_args", "entry_added", RuntimeSync, func(i *Inputs) { i.Runtime.Launch.Args = append(i.Runtime.Launch.Args, "--new") }},
		{"continue args", "continue_args", "entry_added", RuntimeSync, func(i *Inputs) { i.Runtime.Launch.Continue = append(i.Runtime.Launch.Continue, "--new") }},
		{"harness args", "harness_args", "entry_added", RuntimeSync, func(i *Inputs) { i.Runtime.Args = append(i.Runtime.Args, "--new") }},
		{"shell", "shell", "entry_added", RuntimeSync, func(i *Inputs) { i.Runtime.Shell = []string{"zsh"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := cloneInputs(t, s.Inputs)
			tt.edit(&next)
			report := CompareInputs(s.Inputs, next)
			if report.Change != tt.change || !hasInputChange(report, tt.field, tt.code) {
				t.Fatalf("expected %s %s/%s, got %+v", tt.change, tt.field, tt.code, report)
			}
			if report.Change != Compare(s.Inputs.Fingerprints(), next.Fingerprints()) {
				t.Fatal("explanation has competing action rules")
			}
		})
	}
}

func TestDetailedComparisonRoundTripProvenanceAndDeduplication(t *testing.T) {
	s, _ := driftFixture(t)
	next := cloneInputs(t, s.Inputs)
	if s.Inputs.Fingerprints() != next.Fingerprints() {
		t.Fatal("serialization changed fingerprints")
	}
	if report := CompareInputs(s.Inputs, next); report.Change != NoChange || len(report.PendingInputChanges) != 0 {
		t.Fatal(report)
	}
	next.Image.Stages[0].Dockerfile.Source = "/different/Dockerfile"
	next.Image.Stages[0].Ignore.Source = "/different/.dockerignore"
	next.Image.Definition.Source = "/different/harness.json"
	next.Container.Setup[0].Source = "/different/setup.sh"
	next.Runtime.BeforeOpen[0].Source = "/different/before-open.sh"
	for name, f := range next.Image.Stages[0].Context {
		f.Source = filepath.Join("/different", name)
		next.Image.Stages[0].Context[name] = f
	}
	for name, f := range next.Runtime.Files {
		f.Source = filepath.Join("/different", name)
		next.Runtime.Files[name] = f
	}
	if report := CompareInputs(s.Inputs, next); report.Change != NoChange || len(report.PendingInputChanges) != 0 {
		t.Fatal("source-only move caused drift", report)
	}
	next = cloneInputs(t, s.Inputs)
	next.Image.Stages[0].Dockerfile.Hash = Fingerprint("test", "new")
	file := next.Image.Stages[0].Context["Dockerfile"]
	file.Hash = next.Image.Stages[0].Dockerfile.Hash
	next.Image.Stages[0].Context["Dockerfile"] = file
	next.Container.Network = "host"
	next.Runtime.Launch.Continue = append(next.Runtime.Launch.Continue, "--new")
	report := CompareInputs(s.Inputs, next)
	if report.Change != RebuildAndRecreate || len(report.PendingInputChanges) != 3 || len(report.PendingCreationChanges()) != 2 {
		t.Fatal("duplicated image propagation or file reason", report)
	}
	for range 10 {
		if !reflect.DeepEqual(report, CompareInputs(s.Inputs, next)) {
			t.Fatal("unstable reason order")
		}
	}
}

func TestInputSnapshotsAndReportsNeverExposeEnvOrFileContents(t *testing.T) {
	s, q := driftFixture(t)
	overlay := t.TempDir()
	putBuild(t, filepath.Join(overlay, "config.json"), `{"env":["TOKEN=new-secret"],"docker_args":["--env=RAW=new-raw-secret"]}`)
	q.Sources = append(q.Sources, config.Reference{Label: "overlay", Kind: config.ReferenceFixed, Path: overlay})
	next, err := Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	report := CompareInputs(s.Inputs, next.Inputs)
	if !hasInputChange(report, "env", "value_changed") {
		t.Fatal("env difference missing", report)
	}
	for _, r := range report.PendingInputChanges {
		if r.Field == "env" && (r.Before != nil || r.After != nil) {
			t.Fatal("env reason contains values or hashes", r)
		}
	}
	for _, v := range []any{s.Inputs, next.Inputs, report} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"old-secret", "new-secret", "raw-secret", "file-secret"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("secret %q leaked", secret)
			}
		}
	}
	for _, r := range report.PendingInputChanges {
		if strings.Contains(r.String(), "secret") || strings.Contains(r.String(), s.Inputs.Container.Env["TOKEN"]) {
			t.Fatal("human report leaked private input", r)
		}
	}
}

func TestBuiltinConfigInputChangesKeepDistinctFileNames(t *testing.T) {
	s, _ := driftFixture(t)
	before := cloneInputs(t, s.Inputs)
	for _, name := range []string{"builtin-one.json", "builtin-two.json"} {
		before.Runtime.Files[name] = fileInput("test", "builtin", []byte("old"), 0, false)
	}
	after := cloneInputs(t, before)
	for _, name := range []string{"builtin-one.json", "builtin-two.json"} {
		after.Runtime.Files[name] = fileInput("test", "builtin", []byte("new"), 0, false)
	}
	report := CompareInputs(before, after)
	if report.Change != RuntimeSync || len(report.PendingInputChanges) != 2 {
		t.Fatal("shared builtin origin collapsed distinct config changes", report)
	}
	for _, inputChange := range report.PendingInputChanges {
		if inputChange.Key == "" || !strings.Contains(inputChange.String(), inputChange.Key) {
			t.Fatal("builtin change lost its file name", inputChange)
		}
	}
}

func TestInputChangeTextEscapesExternalText(t *testing.T) {
	before, after := "old\nforged", "new\x1b[31m"
	r := InputChange{Scope: ContainerScope, Code: "value_changed", Field: "network", Before: &before, After: &after}
	if text := r.String(); strings.ContainsAny(text, "\n\x1b") || !strings.Contains(text, `\n`) || !strings.Contains(text, `\x1b`) {
		t.Fatal(text)
	}
	r = InputChange{Code: "file_mode_changed", Field: "build_context", Path: "/work/evil\npath", Before: ptr("0644"), After: ptr("0755")}
	if text := r.String(); strings.Contains(text, "\n") || !strings.Contains(text, "permissions: 0644 -> 0755") {
		t.Fatal(text)
	}
}

func ptr(s string) *string { return &s }
