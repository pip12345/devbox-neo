package migration

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func nestedSession(t *testing.T, root, harness string, aliases bool) {
	t.Helper()
	for _, pair := range [][2]string{
		{harness, "harnesses/" + harness},
		{".staged-harness", ".internal/harness-config/staged"},
		{".fallback-defaults", ".internal/harness-config/fallback"},
		{"proxy-ca", ".internal/proxy-ca"},
	} {
		from, to := filepath.Join(root, pair[0]), filepath.Join(root, pair[1])
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(from); os.IsNotExist(err) {
			if err := os.MkdirAll(to, 0700); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		} else if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
		if aliases {
			if err := os.Symlink(pair[1], from); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".internal/leases/attached"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestNestedLayoutsDiscoverWithoutPayloadReadsAndCopyOnce(t *testing.T) {
	for _, harness := range []string{"pi", "opencode"} {
		for _, aliases := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/aliases=%t", harness, aliases), func(t *testing.T) {
				p, name := fixture(t)
				if harness == "opencode" {
					workspace := filepath.Join(filepath.Dir(p.Source), "oc")
					if err := os.Mkdir(workspace, 0700); err != nil {
						t.Fatal(err)
					}
					name = addSession(t, p, workspace, "work", harness, strings.Repeat("c", 32), false)
				}
				root := filepath.Join(p.Source, "sessions", name)
				put(t, filepath.Join(root, ".staged-harness", harness, "user folder/item"), "projected-private")
				if harness == "pi" {
					if err := os.Symlink("/devbox/harness-config/user folder", filepath.Join(root, harness, "user folder")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("/devbox/harness-config/obsolete", filepath.Join(root, harness, "obsolete")); err != nil {
						t.Fatal(err)
					}
				}
				nestedSession(t, root, harness, aliases)
				before, err := stateHash(t.Context(), root)
				if err != nil {
					t.Fatal(err)
				}
				readPayload := watchPayloadReads(t, filepath.Join(root, "harnesses", harness), filepath.Join(root, ".internal/harness-config/staged", harness))
				v := inventory(t, p)
				if readPayload() {
					t.Fatal("discovery opened nested payloads")
				}
				if _, err := v.Select(Selection{}); err != nil {
					t.Fatal(err, v.item("session:"+name).Issues)
				}
				absent(t, p.Work)
				j := mustStage(t, v)
				i := j.Inventory.item("session:" + name)
				seen := map[string]bool{}
				for _, f := range j.Inventory.Files {
					if seen[f.Relative] {
						t.Fatal("duplicate destination", f.Relative)
					}
					seen[f.Relative] = true
				}
				base := filepath.Join(p.Work, "staged-home/sessions", i.Target, "harnesses", harness, "stores")
				if harness == "pi" {
					if read(t, filepath.Join(base, "home/user folder/item")) != "projected-private" {
						t.Fatal("projection lost")
					}
					if read(t, filepath.Join(base, "home/sessions/history.jsonl")) != "fixture-conversation-private" {
						t.Fatal("history lost")
					}
					requireText(t, strings.Join(i.Changes, " "), "Omit stale generated config link", "obsolete")
				} else {
					for _, f := range []string{"opencode.db", "opencode.db-wal", "opencode.db-shm"} {
						if read(t, filepath.Join(base, "data", f)) == "" {
							t.Fatal("database companion lost")
						}
					}
				}
				after, err := stateHash(t.Context(), root)
				if err != nil || after != before {
					t.Fatal("source layout was mutated", err)
				}
			})
		}
	}
}

func TestPartiallyMovedLayoutUsesEachAuthoritativeDirectory(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	if err := os.Mkdir(filepath.Join(root, "harnesses"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "pi"), filepath.Join(root, "harnesses/pi")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, ".staged-harness/pi/custom.txt"), "flat-stage-private")
	if err := os.Symlink("/devbox/harness-config/custom.txt", filepath.Join(root, "harnesses/pi/custom.txt")); err != nil {
		t.Fatal(err)
	}
	j := mustStage(t, inventory(t, p))
	if read(t, filepath.Join(p.Work, "staged-home/sessions", j.Inventory.item("session:"+name).Target, "harnesses/pi/stores/home/custom.txt")) != "flat-stage-private" {
		t.Fatal("guessed whole-session layout")
	}
}

func TestLayoutRejectsAmbiguityEscapesAndUnknownInternalData(t *testing.T) {
	for _, kind := range []string{"duplicate-store", "duplicate-stage", "external-store", "missing-store", "nested-link", "internal-link", "unknown-internal", "unknown-harness", "unknown-control", "foreign-stage-alias"} {
		t.Run(kind, func(t *testing.T) {
			p, name := fixture(t)
			root := filepath.Join(p.Source, "sessions", name)
			nestedSession(t, root, "pi", true)
			remove := func(path string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			}
			link := func(target, path string) {
				t.Helper()
				if err := os.Symlink(target, filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "duplicate-store":
				remove("pi")
				put(t, filepath.Join(root, "pi/other"), "do-not-merge")
			case "duplicate-stage":
				remove(".staged-harness")
				put(t, filepath.Join(root, ".staged-harness/pi/other"), "do-not-prefer")
			case "external-store":
				remove("pi")
				link(t.TempDir(), "pi")
			case "missing-store":
				if err := os.RemoveAll(filepath.Join(root, "harnesses/pi")); err != nil {
					t.Fatal(err)
				}
			case "nested-link":
				if err := os.RemoveAll(filepath.Join(root, "harnesses/pi")); err != nil {
					t.Fatal(err)
				}
				link(t.TempDir(), "harnesses/pi")
			case "internal-link":
				if err := os.RemoveAll(filepath.Join(root, ".internal")); err != nil {
					t.Fatal(err)
				}
				link(t.TempDir(), ".internal")
			case "unknown-internal":
				put(t, filepath.Join(root, ".internal/user-data/file"), "do-not-drop")
			case "unknown-harness":
				put(t, filepath.Join(root, "harnesses/unknown/history"), "do-not-drop")
			case "unknown-control":
				put(t, filepath.Join(root, ".internal/harness-config/unknown/file"), "do-not-drop")
			case "foreign-stage-alias":
				remove(".staged-harness")
				link(t.TempDir(), ".staged-harness")
			}
			v := inventory(t, p)
			if len(v.item("session:"+name).Issues) == 0 {
				t.Fatal("unsafe layout accepted")
			}
			if _, err := Stage(t.Context(), v, Selection{}, &fakeSource{}, nil); err == nil {
				t.Fatal("unsafe layout staged")
			}
			absent(t, p.Work)
		})
	}
}

func TestNestedLeasesBlockEvenExcludedSessions(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	nestedSession(t, root, "pi", true)
	put(t, filepath.Join(root, ".internal/leases/attached/live.json"), fmt.Sprintf(`{"pid":%d}`, os.Getpid()))
	_, err := Stage(t.Context(), inventory(t, p), Selection{Skip: []string{"session:" + name}}, &fakeSource{}, nil)
	if err == nil {
		t.Fatal("ignored nested writer")
	}
	requireText(t, err.Error(), "active attached commands")
	absent(t, p.Work)
}

func TestNestedAliasChangesInvalidateInterruptedSnapshot(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	nestedSession(t, root, "pi", true)
	j, err := Stage(t.Context(), inventory(t, p), Selection{}, &fakeSource{check: func(n int) error {
		if n == 2 {
			return errors.New("interrupted")
		}
		return nil
	}}, nil)
	if err == nil || j == nil {
		t.Fatal("expected interruption", err)
	}
	if err := os.Remove(filepath.Join(root, "pi")); err != nil {
		t.Fatal(err)
	}
	// Even an equivalent absolute alias changes the reviewed source snapshot.
	if err := os.Symlink(filepath.Join(root, "harnesses/pi"), filepath.Join(root, "pi")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeStage(t.Context(), p, &fakeSource{}, nil); err == nil {
		t.Fatal("changed alias accepted")
	}
}

func TestNestedLayoutMergeCapturesConfigAndResumes(t *testing.T) {
	p, name := fixture(t)
	if _, err := store.Open(t.Context(), p.Destination); err != nil {
		t.Fatal(err)
	}
	piRoot := filepath.Join(p.Source, "sessions", name)
	nestedSession(t, piRoot, "pi", true)
	workspace := filepath.Join(filepath.Dir(p.Source), "oc")
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"version":1,"harness":"opencode"}`)
	oc := addSession(t, p, workspace, "", "opencode", strings.Repeat("d", 32), true)
	ocRoot := filepath.Join(p.Source, "sessions", oc)
	put(t, filepath.Join(ocRoot, ".staged-harness/opencode/custom.json"), `{"user":"private-config"}`)
	nestedSession(t, ocRoot, "opencode", false)
	legacyMetadata(t, p, oc, 3, "missing")
	j := mustStage(t, inventory(t, p))
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, h := range []*tar.Header{
		{Name: "./", Typeflag: tar.TypeDir, Mode: 0700},
		{Name: "./custom.json", Typeflag: tar.TypeSymlink, Linkname: "/devbox/harness-config/custom.json"},
		{Name: "./gone", Typeflag: tar.TypeSymlink, Linkname: "/devbox/harness-config/gone"},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	d := &dockertest.Daemon{Containers: map[string]docker.Container{oc: {ID: strings.Repeat("e", 64), Name: "/" + oc}}, Images: map[string]docker.Image{}, Volumes: map[string]bool{}}
	m := Merger{Docker: docker.Runtime{Runner: archiveRunner{d, archive.Bytes()}}, UID: 1000, GID: 1000, Host: map[string]string{"HOME": filepath.Dir(p.Source), "MODEL": "fixture-model", "PASSTHROUGH": "present"}}
	var err error
	j, err = m.CaptureConfigs(t.Context(), p, MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Captures["session:"+oc].Changes) != 1 {
		t.Fatal("captured omission not recorded")
	}
	plan := approvedPlan(t, m, j, MergeChoices{Projects: []string{"project:" + workspace}})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "record-committed:") {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err = m.Apply(t.Context(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	m.Fault = nil
	j, err = m.Resume(t.Context(), p)
	if err != nil || j.Phase != "completed" {
		t.Fatal("resume failed", err)
	}
	for _, job := range plan.Sessions {
		base := filepath.Join(p.Destination, "sessions", job.Identity.Name, "harnesses")
		if job.Item == "session:"+oc {
			if read(t, filepath.Join(base, "opencode/stores/config/custom.json")) != `{"user":"private-config"}` {
				t.Fatal("captured config lost")
			}
			for _, f := range []string{"opencode.db", "opencode.db-wal", "opencode.db-shm"} {
				if read(t, filepath.Join(base, "opencode/stores/data", f)) == "" {
					t.Fatal("OpenCode data lost")
				}
			}
		} else if read(t, filepath.Join(base, "pi/stores/home/sessions/history.jsonl")) != "fixture-conversation-private" {
			t.Fatal("Pi history lost")
		}
	}
	if read(t, filepath.Join(p.Destination, "keep.txt")) != "existing Neo data" {
		t.Fatal("existing data lost")
	}
	if _, err := os.Readlink(filepath.Join(piRoot, "pi")); err != nil {
		t.Fatal("source alias changed", err)
	}
	for _, f := range []string{"journal.json", "report.txt"} {
		if strings.Contains(read(t, filepath.Join(p.Work, f)), "private-config") {
			t.Fatal("config contents leaked")
		}
	}
}
