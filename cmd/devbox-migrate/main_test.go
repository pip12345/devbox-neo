package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/migration"
	"devbox/internal/store"
)

type sourceFake struct{ calls int }

func (s *sourceFake) CheckIdle(context.Context, *migration.Inventory, map[string]string) error {
	s.calls++
	return nil
}
func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func homes(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEVBOX_HOME", filepath.Join(home, "must-not-use"))
	source := filepath.Join(home, ".devbox")
	destination := filepath.Join(home, ".devbox-neo")
	put(t, filepath.Join(source, "state/installation-id"), strings.Repeat("a", 32))
	put(t, filepath.Join(source, "global.json"), `{"version":2,"default_profile":"work","default_harness":"pi","global_env":["TOKEN=private-value"],"proxy_enabled":true,"ignore_project_overrides":false,"harnesses":{}}`)
	put(t, filepath.Join(source, "profiles/work/config.json"), `{"version":1,"harness":"pi","host_network":false}`)
	put(t, filepath.Join(destination, "keep"), "existing-data")
	return source, destination
}
func execute(t *testing.T, interactive bool, input string, args ...string) (string, error, *sourceFake) {
	t.Helper()
	return executeWithMerger(t, migration.Merger{}, interactive, input, args...)
}
func executeWithMerger(t *testing.T, merger migration.Merger, interactive bool, input string, args ...string) (string, error, *sourceFake) {
	t.Helper()
	fake := &sourceFake{}
	cmd := newCommand(fake, merger, interactive)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err, fake
}
func notExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected path: %s (%v)", path, err)
	}
}
func TestHelpDryRunAndUnavailableMergeMakeNoChanges(t *testing.T) {
	for _, args := range [][]string{nil, {"--dry-run"}, {"--merge"}, {"--dry-run", "--stage"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			source, destination := homes(t)
			out, err, fake := execute(t, false, "", args...)
			if len(args) == 1 && args[0] == "--merge" {
				if err == nil {
					t.Fatal(err)
				}
			} else if len(args) > 1 {
				if err == nil {
					t.Fatal("accepted two operations")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "private-value") {
				t.Fatal("leaked config")
			}
			if len(args) == 1 && args[0] == "--dry-run" {
				for _, text := range []string{"[Metadata OK]", "Planned staging path:", "No data has been copied"} {
					if !strings.Contains(out, text) {
						t.Fatal("unclear dry-run output", out)
					}
				}
				if strings.Contains(out, "[Staged]") || strings.Contains(out, "[Inventoried]") {
					t.Fatal("misleading preview status", out)
				}
			}
			if fake.calls != 0 {
				t.Fatal("read-only operation inspected Docker")
			}
			notExist(t, destination+".migration")
			notExist(t, filepath.Join(source, "state/locks"))
			notExist(t, os.Getenv("DEVBOX_HOME"))
		})
	}
}
func TestVerboseInventoryAndSavedReportKeepFullDetails(t *testing.T) {
	source, destination := homes(t)
	compact, err, runtime := execute(t, false, "", "--dry-run")
	if err != nil || runtime.calls != 0 {
		t.Fatal(err)
	}
	full, err, runtime := execute(t, false, "", "--dry-run", "--verbose")
	if err != nil || runtime.calls != 0 {
		t.Fatal(err)
	}
	detail := "Path: " + filepath.Join(source, "profiles/work")
	if strings.Contains(compact, detail) || !strings.Contains(full, detail) {
		t.Fatal("verbosity did not control inventory details")
	}
	menu, err, runtime := execute(t, true, "1\n", "--verbose")
	if err != nil || runtime.calls != 0 || !strings.HasSuffix(menu, full) {
		t.Fatal("menu lost verbose setting", err)
	}
	notExist(t, destination+".migration")
	notExist(t, filepath.Join(source, "state/locks"))
	if out, err, _ := execute(t, false, "", "--stage", "--confirm-stage"); err != nil {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(filepath.Join(destination+".migration", "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), detail) {
		t.Fatal("saved report lost full details")
	}
}

func TestStageMenuShowsWarningsBeforeConfirmation(t *testing.T) {
	source, destination := homes(t)
	p, err := migration.NewPaths(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	v, err := migration.InventorySource(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	warning := "Not imported: retained unsupported store; original remains untouched."
	v.Items[0].Warnings = []string{warning}
	fake := &sourceFake{}
	cmd := newCommand(fake, migration.Merger{}, true)
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("3\nn\n0\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := stageMenu(cmd, fake, v, migration.Selection{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	warnAt, confirmAt := strings.Index(text, "Warning: "+warning), strings.Index(text, "Prepare this scope")
	if warnAt < 0 || confirmAt < warnAt {
		t.Fatal("warning hidden behind confirmation", text)
	}
	if fake.calls != 0 {
		t.Fatal("cancelled preparation inspected Docker")
	}
	notExist(t, p.Work)
}

func TestNonInteractiveRequiresExplicitStageApproval(t *testing.T) {
	_, destination := homes(t)
	out, err, _ := execute(t, false, "", "--stage")
	if err == nil || !strings.Contains(err.Error(), "--confirm-stage") {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Staging preview") {
		t.Fatal(out)
	}
	notExist(t, destination+".migration")
	out, err, fake := execute(t, false, "", "--stage", "--confirm-stage")
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls < 2 || !strings.Contains(out, "No sessions have been imported") {
		t.Fatal(out)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "keep")); err != nil || string(data) != "existing-data" {
		t.Fatal("changed destination")
	}
	if _, err := os.Stat(filepath.Join(destination+".migration", "report.txt")); err != nil {
		t.Fatal(err)
	}
	_, err, _ = execute(t, false, "", "--resume")
	if err == nil || !strings.Contains(err.Error(), "does not authorize merge") {
		t.Fatal(err)
	}
}
func TestUnsupportedProfilesNeedScriptSkipButMenuReviewsExclusions(t *testing.T) {
	source, destination := homes(t)
	put(t, filepath.Join(source, "profiles/codex/config.json"), `{"version":1,"harness":"codex"}`)
	_, err, _ := execute(t, false, "", "--stage", "--confirm-stage")
	if err == nil {
		t.Fatal("silently skipped unsupported profile")
	}
	notExist(t, destination+".migration")
	out, err, _ := execute(t, true, "3\ny\n", "--stage")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Skip profile:codex", "including the listed exclusions", "[Skipped] profile:codex"} {
		if !strings.Contains(out, text) {
			t.Fatalf("missing %q: %s", text, out)
		}
	}
	notExist(t, filepath.Join(destination+".migration", "staged-home/profiles/codex"))
}
func TestInteractiveCancelAndEOFDoNotStage(t *testing.T) {
	for _, input := range []string{"0\n", "q\n", "", "3\nn\n0\n", "2\n0\n0\n", "4\n0\n"} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			_, destination := homes(t)
			_, _, fake := execute(t, true, input, "--stage")
			notExist(t, destination+".migration")
			if fake.calls != 0 {
				t.Fatal("cancel reached source runtime")
			}
		})
	}
}
func TestExplicitMergeDoesNotApplyWithoutConfirmation(t *testing.T) {
	_, destination := homes(t)
	if _, err := store.Open(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if _, err, _ := execute(t, false, "", "--stage", "--confirm-stage"); err != nil {
		t.Fatal(err)
	}
	d := &dockertest.Daemon{}
	run := func(args ...string) (string, error) {
		cmd := newCommand(&sourceFake{}, migration.Merger{Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000}, false)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := run("--merge")
	if err == nil || !strings.Contains(err.Error(), "--confirm-merge") {
		t.Fatal(out, err)
	}
	notExist(t, filepath.Join(destination, "profiles/work"))
	out, err = run("--merge", "--confirm-merge")
	if err != nil {
		t.Fatal(out, err)
	}
	if _, err = os.Stat(filepath.Join(destination, "profiles/work/config.json")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "private-value") {
		t.Fatal("merge output leaked config")
	}
	if !strings.Contains(out, "completed") {
		t.Fatal(out)
	}
}
func TestResumeScopeCannotChangeAndCustomEndpointsAreExplicit(t *testing.T) {
	source, destination := homes(t)
	for _, args := range [][]string{{"--resume", "--confirm-stage"}, {"--dry-run", "--skip", "profile:work"}, {"--resume", "--caches"}} {
		if _, err, _ := execute(t, false, "", args...); err == nil {
			t.Fatalf("accepted bad scope: %v", args)
		}
	}
	_, err, _ := execute(t, false, "", "--source", source, "--destination", destination, "--stage", "--confirm-stage")
	if err != nil {
		t.Fatal(err)
	}
	_, err, _ = execute(t, false, "", "--source", source, "--destination", destination, "--stage", "--confirm-stage")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
}
