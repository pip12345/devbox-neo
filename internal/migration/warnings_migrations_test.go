package migration

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func TestUnsupportedRetainedStoresWarnWithoutBlockingSupportedImport(t *testing.T) {
	for _, harness := range []string{"claude", "codex", "copilot"} {
		for _, layout := range []string{"flat", "nested"} {
			t.Run(harness+"/"+layout, func(t *testing.T) {
				p, name := fixture(t)
				root := filepath.Join(p.Source, "sessions", name)
				retained := filepath.Join(root, harness)
				if layout == "nested" {
					nestedSession(t, root, "pi", true)
					retained = filepath.Join(root, "harnesses", harness)
				}
				payload := filepath.Join(retained, "history/private.json")
				put(t, payload, "unsupported-private-history")
				if layout == "nested" {
					if err := os.Symlink("harnesses/"+harness, filepath.Join(root, harness)); err != nil {
						t.Fatal(err)
					}
				}
				readPayload := watchPayloadReads(t, retained, payload)
				v := inventory(t, p)
				item := v.item("session:" + name)
				if len(item.Issues) != 0 || len(item.Warnings) != 1 {
					t.Fatal(item.Issues, item.Warnings)
				}
				requireText(t, item.Warnings[0], "Not imported", harness, retained, "remain untouched")
				var report bytes.Buffer
				if err := Report(&report, v, nil); err != nil {
					t.Fatal(err)
				}
				requireText(t, report.String(), "[Warning] session:"+name, "Warning: Not imported", harness)
				j := mustStage(t, v)
				if readPayload() {
					t.Fatal("read unsupported payload instead of leaving it untouched")
				}
				if _, err := store.Open(t.Context(), p.Destination); err != nil {
					t.Fatal(err)
				}
				d := &dockertest.Daemon{Containers: map[string]docker.Container{}, Images: map[string]docker.Image{}, Volumes: map[string]bool{}}
				m := Merger{Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000, Host: map[string]string{"HOME": filepath.Dir(p.Source), "MODEL": "fixture-model", "PASSTHROUGH": "present"}}
				plan := approvedPlan(t, m, j, MergeChoices{})
				if len(plan.Warnings) != 1 {
					t.Fatal("merge lost warning", plan.Warnings)
				}
				for _, review := range plan.Reviews {
					if strings.Contains(review.Message, retained) {
						t.Fatal("warning became an extra acceptance gate")
					}
				}
				report.Reset()
				if err := ReviewReport(&report, plan); err != nil {
					t.Fatal(err)
				}
				requireText(t, report.String(), "Warning:", harness, retained)
				if _, err := m.Apply(t.Context(), p, plan); err != nil {
					t.Fatal(err)
				}
				if readPayload() {
					t.Fatal("merge accessed unsupported payload")
				}
				if read(t, payload) != "unsupported-private-history" {
					t.Fatal("source changed")
				}
				base := filepath.Join(p.Destination, "sessions", plan.Sessions[0].Identity.Name, "harnesses")
				absent(t, filepath.Join(base, harness))
				if read(t, filepath.Join(base, "pi/stores/home/sessions/history.jsonl")) != "fixture-conversation-private" {
					t.Fatal("Pi history lost")
				}
				for _, f := range []string{"journal.json", "report.txt"} {
					text := read(t, filepath.Join(p.Work, f))
					requireText(t, text, "Not imported", harness)
					if strings.Contains(text, "unsupported-private-history") {
						t.Fatal("payload leaked")
					}
				}
			})
		}
	}
}

func TestUnsupportedRecordedHarnessStillBlocks(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	put(t, filepath.Join(root, "claude/history"), "unsupported-private")
	rewriteObject(t, filepath.Join(root, "metadata.json"), func(raw map[string]json.RawMessage) {
		raw["harness"] = []byte(`"claude"`)
		var creation map[string]json.RawMessage
		if err := json.Unmarshal(raw["creation_settings"], &creation); err != nil {
			t.Fatal(err)
		}
		creation["harness"] = []byte(`"claude"`)
		raw["creation_settings"] = encode(creation)
	})
	v := inventory(t, p)
	requireText(t, strings.Join(v.item("session:"+name).Issues, " "), "Unsupported recorded harness")
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("unsupported active harness imported")
	}
}

func TestUnsupportedRetainedStoreDoesNotPermitForeignAlias(t *testing.T) {
	p, name := fixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(p.Source, "sessions", name, "claude")); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("foreign alias turned into warning")
	}
}
