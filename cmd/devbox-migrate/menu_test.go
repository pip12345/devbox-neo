package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/migration"
	"devbox/internal/store"
)

func TestEntryMenuIsCompactAndReadOnly(t *testing.T) {
	for _, input := range []string{"0\n", "q\n", "wrong\n3\n4\n0\n", ""} {
		t.Run(input, func(t *testing.T) {
			source, destination := homes(t)
			out, err, runtime := execute(t, true, input)
			if input == "" {
				if !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"   [1]  Preview migration (read-only)", "   [2]  Prepare staged copy", "   [3]  Review and import (not staged)", "   [4]  Resume (nothing pending)", "   [0]  Exit", "   Choose a number > "} {
				if !strings.Contains(out, text) {
					t.Fatalf("missing %q: %s", text, out)
				}
			}
			for _, extra := range []string{"--replace-auth", "private-value", "Neither installation", "Start with option", destination + ".migration"} {
				if strings.Contains(out, extra) {
					t.Fatal("landing menu includes action details", out)
				}
			}
			if input == "0\n" && strings.Count(out, "\n") > 16 {
				t.Fatal("landing menu is too verbose", out)
			}
			if runtime.calls != 0 {
				t.Fatal("entry menu inspected Docker")
			}
			notExist(t, destination+".migration")
			notExist(t, filepath.Join(source, "state/locks"))
		})
	}
}

func TestEntryMenuPreviewUsesDryRunWithoutMutations(t *testing.T) {
	for _, state := range []string{"absent", "prepared", "unreadable"} {
		t.Run(state, func(t *testing.T) {
			var source, destination string
			if state == "prepared" {
				p, _ := preparedMenuFixture(t)
				source, destination = p.Source, p.Destination
			} else {
				source, destination = homes(t)
			}
			work := destination + ".migration"
			if state == "unreadable" {
				put(t, filepath.Join(work, "keep"), "unrelated")
			}
			snapshot := func(root string) map[string]string {
				t.Helper()
				files := map[string]string{}
				if _, err := os.Stat(root); os.IsNotExist(err) {
					return files
				}
				err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() {
						files[path] = "directory"
						return nil
					}
					b, err := os.ReadFile(path)
					files[path] = string(b)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				return files
			}
			beforeSource, beforeWork, beforeDestination := snapshot(source), snapshot(work), snapshot(destination)
			out, err, runtime := execute(t, true, "1\n")
			if err != nil {
				t.Fatal(err)
			}
			explicit, err, explicitRuntime := execute(t, false, "", "--dry-run")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(out, explicit) {
				t.Fatal("menu preview diverged from dry-run")
			}
			if runtime.calls != 0 || explicitRuntime.calls != 0 {
				t.Fatal("preview inspected Docker")
			}
			for _, pair := range [][2]map[string]string{{beforeSource, snapshot(source)}, {beforeWork, snapshot(work)}, {beforeDestination, snapshot(destination)}} {
				if !reflect.DeepEqual(pair[0], pair[1]) {
					t.Fatal("preview mutated files")
				}
			}
			if state == "absent" {
				notExist(t, work)
				notExist(t, filepath.Join(source, "state/locks"))
			}
		})
	}
}

func TestEntryMenuStagesOnlyAfterItsOwnApproval(t *testing.T) {
	for _, test := range []struct {
		input    string
		prepared bool
	}{
		{"2\n0\n", false},
		{"2\n3\nn\n0\n", false},
		{"2\n3\ny\n", true},
	} {
		t.Run(test.input, func(t *testing.T) {
			source, destination := homes(t)
			out, err, runtime := execute(t, true, test.input)
			if err != nil {
				t.Fatal(out, err)
			}
			if test.prepared {
				p, err := migration.NewPaths(source, destination)
				if err != nil {
					t.Fatal(err)
				}
				j, err := migration.Load(p)
				if err != nil {
					t.Fatal(err)
				}
				if j.Phase != "prepared" || j.Merge != nil {
					t.Fatal("staging advanced into merge")
				}
				if !strings.Contains(out, "Prepare this scope") || runtime.calls == 0 {
					t.Fatal(out)
				}
			} else {
				notExist(t, destination+".migration")
				if runtime.calls != 0 {
					t.Fatal("cancel reached source runtime")
				}
			}
			entries, err := os.ReadDir(destination)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "keep" {
				t.Fatal("staging changed destination")
			}
		})
	}
}

func preparedMenuFixture(t *testing.T) (migration.Paths, migration.Merger) {
	t.Helper()
	source, destination := homes(t)
	if _, err := store.Open(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if out, err, _ := execute(t, false, "", "--stage", "--confirm-stage"); err != nil {
		t.Fatal(out, err)
	}
	paths, err := migration.NewPaths(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	return paths, migration.Merger{Docker: docker.Runtime{Runner: &dockertest.Daemon{}}, UID: 1000, GID: 1000}
}

func TestEntryMenuPreparedImportStillRequiresMergeApproval(t *testing.T) {
	paths, merger := preparedMenuFixture(t)
	for _, input := range []string{"2\n4\n0\n", "3\n0\n", "3\n4\ny\n5\nn\n0\n"} {
		out, err, _ := executeWithMerger(t, merger, true, input)
		if err != nil {
			t.Fatal(out, err)
		}
		notExist(t, filepath.Join(paths.Destination, "configs/work"))
		j, err := migration.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if j.Merge != nil {
			t.Fatal("entering merge review authorized import")
		}
	}
	out, err, _ := executeWithMerger(t, merger, true, "3\n4\ny\n5\ny\n")
	if err != nil {
		t.Fatal(out, err)
	}
	if !strings.Contains(out, "Apply this exact merge plan?") {
		t.Fatal(out)
	}
	j, err := migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if j.Phase != "completed" {
		t.Fatal(j.Phase)
	}
	out, err, runtime := execute(t, true, "2\n3\n4\n0\n")
	if err != nil || !strings.Contains(out, "Resume (completed)") {
		t.Fatal(out, err)
	}
	if runtime.calls != 0 {
		t.Fatal("completed migration reached source runtime")
	}
}

func TestEntryMenuResumesInterruptedStaging(t *testing.T) {
	paths, _ := preparedMenuFixture(t)
	j, err := migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = "staging"
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(paths.Work, "journal.json"), string(data))
	out, err, runtime := execute(t, true, "2\n3\n4\n")
	if err != nil || !strings.Contains(out, "Review and import (copy incomplete)") {
		t.Fatal(out, err)
	}
	j, err = migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if j.Phase != "prepared" || j.Merge != nil || runtime.calls == 0 {
		t.Fatal("did not resume staging only")
	}
	notExist(t, filepath.Join(paths.Destination, "configs/work"))
}

func TestEntryMenuResumesApprovedMerge(t *testing.T) {
	paths, merger := preparedMenuFixture(t)
	merger.Fault = func(point string) error {
		if strings.HasPrefix(point, "published:") {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err, _ := executeWithMerger(t, merger, true, "3\n4\ny\n5\ny\n"); err == nil {
		t.Fatal("fault did not trigger")
	}
	j, err := migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if j.Phase != "merging" {
		t.Fatal(j.Phase)
	}
	merger.Fault = nil
	out, err, _ := executeWithMerger(t, merger, true, "2\n3\n4\n")
	if err != nil || !strings.Contains(out, "Review and import (merge incomplete)") {
		t.Fatal(out, err)
	}
	j, err = migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if j.Phase != "completed" {
		t.Fatal(j.Phase)
	}
}

func TestEntryMenuRefusesUnknownWorkDirectory(t *testing.T) {
	source, destination := homes(t)
	put(t, filepath.Join(destination+".migration", "keep"), "unrelated")
	out, err, runtime := execute(t, true, "2\n3\n4\n0\n")
	if err != nil || !strings.Contains(out, "Cannot read migration state") {
		t.Fatal(out, err)
	}
	entries, err := os.ReadDir(destination + ".migration")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatal("unknown work directory mutated")
	}
	if runtime.calls != 0 {
		t.Fatal("unknown migration reached source runtime")
	}
	notExist(t, filepath.Join(source, "state/locks"))
}

func TestEntryMenuUsesExplicitEndpoints(t *testing.T) {
	source, destination := homes(t)
	t.Setenv("HOME", t.TempDir())
	out, err, _ := execute(t, true, "0\n", "--source", source, "--destination", destination)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, source) || !strings.Contains(out, destination) || strings.Contains(out, destination+".migration") {
		t.Fatal(out)
	}
	notExist(t, destination+".migration")
}

func TestMergeOwnerMenusUseBracketedChoicesAndZeroBack(t *testing.T) {
	paths, merger := preparedMenuFixture(t)
	j, err := migration.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, choice string }{
		{"global:config", "Keep the existing imported-global config"},
		{"profile:work", "Choose a different destination config name"},
		{"auth:pi", "Keep existing auth"},
	} {
		n := 0
		found := false
		for _, item := range j.Inventory.Items {
			if j.Excluded[item.Key] != "" {
				continue
			}
			n++
			if item.Key == tc.key {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing fixture item", tc.key)
		}
		input := "2\n" + strconv.Itoa(n) + "\n0\n0\n0\n"
		out, err, _ := executeWithMerger(t, merger, true, input, "--merge")
		if err != nil {
			t.Fatal(out, err)
		}
		for _, text := range []string{"   [1]  " + tc.choice, "   [0]  Back", "   [0]  Cancel", "   Choose a number > "} {
			if !strings.Contains(out, text) {
				t.Fatal(out)
			}
		}
	}
	j, err = migration.Load(paths)
	if err != nil || j.Merge != nil {
		t.Fatal("navigation authorized merge", err)
	}
}

func TestMenuChoicesMatchRewritePresentation(t *testing.T) {
	var out bytes.Buffer
	ui := menu{in: bufio.NewReader(strings.NewReader("0\n")), out: &out}
	answer, err := ui.choose("What would you like to do?", []string{"Review inventory", "Choose what to stage"}, "Back")
	if err != nil || answer != "0" {
		t.Fatal(answer, err)
	}
	want := "\nWhat would you like to do?\n   [1]  Review inventory\n   [2]  Choose what to stage\n\n   [0]  Back\n\n   Choose a number > "
	if out.String() != want {
		t.Fatalf("menu presentation differs:\n%s", out.String())
	}
}

func TestMenuChoiceValidationAndCancellation(t *testing.T) {
	for _, tc := range []struct{ input, answer string }{
		{"bad\n99\n1\n", "1"}, {"q\n", "0"}, {"0\n", "0"}, {"1", ""},
	} {
		var out bytes.Buffer
		ui := menu{in: bufio.NewReader(strings.NewReader(tc.input)), out: &out}
		answer, err := ui.choose("Choose", []string{"Action"}, "Back")
		if answer != tc.answer {
			t.Fatal(answer)
		}
		if tc.answer == "" {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStageSelectionUsesNumberedCacheChoiceAndZeroBack(t *testing.T) {
	var out bytes.Buffer
	ui := menu{in: bufio.NewReader(strings.NewReader("1\n2\n0\n")), out: &out}
	v := &migration.Inventory{Items: []migration.Item{{Key: "profile:work", Harness: "pi"}}}
	var selection migration.Selection
	if err := choose(ui, v, &selection); err != nil {
		t.Fatal(err)
	}
	if !selection.Caches || !has(selection.Skip, "profile:work") {
		t.Fatal(selection)
	}
	for _, text := range []string{"   [1]  [include] [Metadata OK] profile:work", "   [2]  Toggle caches", "   [0]  Back"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal(out.String())
		}
	}
}

func TestStageSelectionShowsMetadataStatusSeparatelyFromSelection(t *testing.T) {
	v := &migration.Inventory{Items: []migration.Item{
		{Key: "global:config", Kind: "global"},
		{Key: "session:warning", Kind: "session", Harness: "pi", Warnings: []string{"Retained unsupported state."}},
		{Key: "session:error", Kind: "session", Harness: "pi", Issues: []string{"Missing workspace."}},
	}}
	var out bytes.Buffer
	s := migration.Selection{Skip: []string{"session:error"}}
	if err := choose(menu{in: bufio.NewReader(strings.NewReader("3\n0\n")), out: &out}, v, &s); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"[include] [Metadata OK] global:config",
		"[include] [Warning] session:warning (pi)",
		"[skip] [Error] session:error (pi)",
		"[include] [Error] session:error (pi)",
	} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("missing status", text, out.String())
		}
	}
	if strings.Contains(out.String(), "()") || strings.Contains(out.String(), "\x1b") {
		t.Fatal("empty harness or ANSI in plain menu")
	}
	if len(s.Skip) != 0 || len(v.Items[2].Issues) != 1 {
		t.Fatal("toggle changed metadata status or failed to update selection")
	}
}

func TestStageSelectionExcludeAll(t *testing.T) {
	v := &migration.Inventory{Items: []migration.Item{
		{Key: "global:config"}, {Key: "profile:work"}, {Key: "session:example"}, {Key: "cache:pi/npm-cache"},
	}}
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"all off", "6\n0\n", []string{"global:config", "profile:work", "session:example", "cache:pi/npm-cache"}},
		{"repeat", "6\n6\n0\n", []string{"global:config", "profile:work", "session:example", "cache:pi/npm-cache"}},
		{"select again", "6\n1\n2\n0\n", []string{"session:example", "cache:pi/npm-cache"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			ui := menu{in: bufio.NewReader(strings.NewReader(tc.input)), out: &out}
			s := migration.Selection{Skip: []string{"profile:work"}, Caches: true, ExternalAuth: []string{"auth:opencode"}}
			if err := choose(ui, v, &s); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.Skip, tc.want) || s.Caches {
				t.Fatal(s)
			}
			if !reflect.DeepEqual(s.ExternalAuth, []string{"auth:opencode"}) {
				t.Fatal("changed unrelated auth approval")
			}
			if !strings.Contains(out.String(), "   [6]  Exclude all items") {
				t.Fatal(out.String())
			}
		})
	}
	var out bytes.Buffer
	s := migration.Selection{Caches: true}
	if err := choose(menu{in: bufio.NewReader(strings.NewReader("2\n0\n")), out: &out}, &migration.Inventory{}, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Skip) != 0 || s.Caches {
		t.Fatal("empty inventory did not turn off caches")
	}
}

func TestExplicitHelpBypassesEntryMenu(t *testing.T) {
	homes(t)
	out, err, runtime := execute(t, true, "", "--help")
	if err != nil || !strings.Contains(out, "--stage") || strings.Contains(out, "[0]  Exit") {
		t.Fatal(out, err)
	}
	if runtime.calls != 0 {
		t.Fatal("help inspected Docker")
	}
}
