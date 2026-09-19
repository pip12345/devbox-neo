package migration

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func TestSourceConfigCompatibility(t *testing.T) {
	for _, version := range []string{``, `"version":null,`, `"version":1,`} {
		g, err := sourceGlobal([]byte(`{` + version + `"default_profile":"work","global_env":["KEY=private"],"pi":{"auth_file":"/external/pi.json"},"opencode":{"auth_file":"/external/oc.json"}}`))
		if err != nil || g.Version != 2 || g.DefaultProfile != "work" || !g.ProxyEnabled || g.GlobalEnv[0] != "KEY=private" || g.Harnesses["pi"]["auth_file"] != "/external/pi.json" || g.Harnesses["opencode"]["auth_file"] != "/external/oc.json" {
			t.Fatalf("legacy global conversion failed: %v", err)
		}
	}
	for _, version := range []string{``, `"version":null,`, `"version":2,`} {
		g, err := sourceGlobal([]byte(`{` + version + `"harnesses":{"pi":{"auth_file":"/external/pi.json"}},"proxy_enabled":false}`))
		if err != nil || g.ProxyEnabled || g.Harnesses["pi"]["auth_file"] != "/external/pi.json" {
			t.Fatalf("global v2 conversion failed: %v", err)
		}
	}
	for _, data := range []string{`{}`, `{"version":null}`, `{"version":1}`} {
		l, _, err := convertLayer([]byte(data))
		if err != nil || l.Shell != nil || l.Harness != nil || l.Network != nil {
			t.Fatalf("sparse inheritance lost: %v", err)
		}
	}
	l, changes, err := convertLayer([]byte(`{"auto_rebuild":true,"proxy_enabled":false,"proxy_allowed_domains":[],"extra_env":["KEY=${env:VALUE}"],"host_network":true}`))
	if err != nil || l.Network == nil || *l.Network != "host" || l.Env[0] != "KEY=${env:VALUE}" || len(changes) != 2 {
		t.Fatalf("legacy layer conversion failed: %v", err)
	}
	for _, data := range []string{
		`{"proxy":{},"proxy_enabled":false}`, `{"proxy":null,"proxy_enabled":false}`,
		`{"proxy":{},"proxy_enabled":null}`, `{"auto_rebuild":"private"}`,
		`{"proxy_allowed_domains":"private"}`, `{"unknown_setting":"private"}`,
	} {
		_, _, err := convertLayer([]byte(data))
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("invalid layer accepted or exposed: %v", err)
		}
	}
	for _, data := range []string{
		`{"version":1,"harnesses":{"pi":{"auth_file":"private"}}}`,
		`{"version":2,"pi":{"auth_file":"private"}}`, `{"version":0}`, `{"version":9}`,
		`{"version":1,"pi":{"auth_file":"private","unknown":"private"}}`,
		`{"version":1,"pi":{"auth_file":"private"},"pi":{}}`,
	} {
		_, err := sourceGlobal([]byte(data))
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("invalid global accepted or exposed: %v", err)
		}
	}
}

func legacyMetadata(t *testing.T, p Paths, name string, version int, ownership string) {
	t.Helper()
	rewriteObject(t, filepath.Join(p.Source, "sessions", name, "metadata.json"), func(raw map[string]json.RawMessage) {
		raw["metadata_version"] = []byte(fmt.Sprint(version))
		if ownership == "missing" {
			delete(raw, "ownership_version")
		} else {
			raw["ownership_version"] = []byte(ownership)
		}
	})
}

func TestSourceSessionCompatibility(t *testing.T) {
	for _, tc := range []struct {
		version              int
		ownership            string
		record, installation bool
	}{
		{3, "missing", true, true}, {3, "0", false, false},
		{4, "null", true, false}, {4, "missing", false, true}, {4, "1", true, true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			p, name := fixture(t)
			legacyMetadata(t, p, name, tc.version, tc.ownership)
			if !tc.record {
				if err := os.Remove(filepath.Join(p.Source, "sessions", name, "session.json")); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.installation {
				if err := os.RemoveAll(filepath.Join(p.Source, "state")); err != nil {
					t.Fatal(err)
				}
			}
			before := treeSnapshot(t, p.Source)
			v := inventory(t, p)
			item := v.item("session:" + name)
			if _, err := v.Select(Selection{}); err != nil {
				t.Fatal(err, item.Issues)
			}
			if item.SessionID != inventory(t, p).item(item.Key).SessionID {
				t.Fatal("unstable proposed ID")
			}
			if tc.record && item.SessionID != strings.Repeat("b", 32) {
				t.Fatal("replaced existing ID")
			}
			if !tc.record && (item.Activity != "" || item.Action != "") {
				t.Fatal("invented activity")
			}
			if !reflect.DeepEqual(before, treeSnapshot(t, p.Source)) {
				t.Fatal("discovery mutated source")
			}
			j := mustStage(t, v)
			if _, err := Load(p); err != nil {
				t.Fatal(err)
			}
			if j.Inventory.item(item.Key).SessionID != item.SessionID {
				t.Fatal("staging changed ID")
			}
			for path, hash := range before {
				if digest([]byte(read(t, filepath.Join(p.Source, path)))) != hash {
					t.Fatal("source changed", path)
				}
			}
			if !tc.record {
				absent(t, filepath.Join(p.Source, "sessions", name, "session.json"))
			}
			if !tc.installation {
				absent(t, filepath.Join(p.Source, "state/installation-id"))
			}
		})
	}
}

func TestMissingConfigFilesRetainOldDefaultAndInheritanceRules(t *testing.T) {
	p, _ := fixture(t)
	for _, path := range []string{"global.json", "profiles/work/config.json"} {
		if err := os.Remove(filepath.Join(p.Source, path)); err != nil {
			t.Fatal(err)
		}
	}
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err != nil {
		t.Fatal(err)
	}
	j := mustStage(t, v)
	if j.Inventory.SourceHashes[filepath.Join(p.Source, "global.json")] != "missing" {
		t.Fatal("missing config not tracked")
	}
	for _, path := range []string{"global.json", "profiles/work/config.json"} {
		absent(t, filepath.Join(p.Source, path))
	}
	p.Source = filepath.Join(filepath.Dir(p.Source), "nonexistent")
	if _, err := InventorySource(t.Context(), p); err == nil {
		t.Fatal("nonexistent installation accepted")
	}
}

func TestLegacyExternalAuthStillRequiresApproval(t *testing.T) {
	p, _ := fixture(t)
	auth := filepath.Join(t.TempDir(), "auth.json")
	put(t, auth, `{"token":"external-private"}`)
	put(t, filepath.Join(p.Source, "global.json"), fmt.Sprintf(`{"version":1,"default_harness":"pi","pi":{"auth_file":%q}}`, auth))
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("legacy auth bypassed approval")
	}
	j, err := Stage(t.Context(), v, Selection{ExternalAuth: []string{"auth:pi"}}, &fakeSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(p.Work, "staged-home/auth/pi/auth.json")) != read(t, auth) {
		t.Fatal("auth lost")
	}
	var report bytes.Buffer
	if err := Report(&report, &j.Inventory, j); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report.String(), "external-private") {
		t.Fatal("auth leaked")
	}
}

func TestMissingSourceIdentityDoesNotAuthorizeLabeledRecords(t *testing.T) {
	p, name := fixture(t)
	if err := os.Remove(filepath.Join(p.Source, "state/installation-id")); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	requireText(t, strings.Join(v.item("session:"+name).Issues, " "), "requires the source state/installation-id")
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("labeled source without identity accepted")
	}
}

func TestLegacyOwnershipAppliesToEverySourceReadAndWriter(t *testing.T) {
	p, name := fixture(t)
	legacyMetadata(t, p, name, 3, "missing")
	v := inventory(t, p)
	c := docker.Container{ID: strings.Repeat("e", 64), Name: "/" + name}
	d := &dockertest.Daemon{Containers: map[string]docker.Container{name: c}}
	runtime := DockerSource{docker.Runtime{Runner: d}}
	if err := runtime.CheckIdle(t.Context(), v, nil); err != nil {
		t.Fatal(err)
	}
	for _, labels := range []map[string]string{
		{"devbox.managed": "true", "devbox.installation_id": "foreign"},
		{"devbox.installation_id": v.Installation}, {"devbox.managed": "false"},
		{"devbox-rewrite.managed": "true"},
	} {
		c.Config.Labels = labels
		d.Containers[name] = c
		if err := runtime.CheckIdle(t.Context(), v, nil); err == nil {
			t.Fatal("foreign/partial labels accepted")
		}
	}
	c.Config.Labels = nil
	c.State.Running = true
	d.Containers[name] = c
	if err := runtime.CheckIdle(t.Context(), v, map[string]string{"session:" + name: "skipped"}); err == nil {
		t.Fatal("excluded legacy writer accepted")
	}
	c.State.Running = false
	d.Containers[name] = c
	m := Merger{Docker: docker.Runtime{Runner: archiveRunner{d, archiveConfig(t)}}}
	if err := os.Mkdir(p.Work, 0700); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Inventory: *v}
	capture, err := m.capture(t.Context(), j, *v.item("session:" + name))
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(capture.Root, "local-plugin.json")) == "" {
		t.Fatal("legacy capture lost data")
	}
	legacyMetadata(t, p, name, 4, "1")
	if err := runtime.CheckIdle(t.Context(), v, nil); err == nil {
		t.Fatal("changed metadata accepted")
	}
	if _, err := m.capture(t.Context(), j, *v.item("session:" + name)); err == nil {
		t.Fatal("capture bypassed source metadata verification")
	}
	v = inventory(t, p)
	if err := runtime.CheckIdle(t.Context(), v, nil); err == nil {
		t.Fatal("current record authorized unlabeled container")
	}
}

func TestLegacyLeaseDirectoryBlocksSnapshot(t *testing.T) {
	p, name := fixture(t)
	put(t, filepath.Join(p.Source, "sessions", name, "active/command.json"), fmt.Sprintf(`{"pid":%d}`, os.Getpid()))
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(t.Context(), v, Selection{}, &fakeSource{}, nil); err == nil {
		t.Fatal("legacy active command ignored")
	}
	absent(t, p.Work)
}

func TestStaleProjectionIsGenericReviewedAndRechecked(t *testing.T) {
	for _, entry := range []string{"custom folder", "user-code/archive", "notes.txt"} {
		t.Run(entry, func(t *testing.T) {
			p, name := fixture(t)
			root := filepath.Join(p.Source, "sessions", name)
			if err := os.MkdirAll(filepath.Join(root, ".staged-harness/pi"), 0700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "pi", entry)
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/devbox/harness-config/"+entry, link); err != nil {
				t.Fatal(err)
			}
			v := inventory(t, p)
			j, err := Stage(t.Context(), v, Selection{}, &fakeSource{check: func(n int) error {
				if n == 2 {
					return errors.New("interrupted")
				}
				return nil
			}}, nil)
			if err == nil || j == nil {
				t.Fatal("expected staged interruption", err)
			}
			item := j.Inventory.item("session:" + name)
			requireText(t, strings.Join(item.Changes, " "), "Omit stale generated config link", entry)
			if _, err := os.Readlink(link); err != nil {
				t.Fatal("source link removed", err)
			}
			put(t, filepath.Join(root, ".staged-harness/pi", entry), "appeared after approval")
			if _, err := ResumeStage(t.Context(), p, &fakeSource{}, nil); err == nil {
				t.Fatal("missing target became present without renewed review")
			}
		})
	}
}

func TestMixedFormatsMergeAndResumePreserveDurableData(t *testing.T) {
	p, name := fixture(t)
	if _, err := store.Open(t.Context(), p.Destination); err != nil {
		t.Fatal(err)
	}
	originalID := read(t, filepath.Join(p.Destination, "state/installation-id"))
	legacyMetadata(t, p, name, 3, "missing")
	if err := os.Remove(filepath.Join(p.Source, "sessions", name, "session.json")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(p.Source, "global.json"), `{"version":1,"default_harness":"pi","pi":{},"opencode":{}}`)
	put(t, filepath.Join(p.Source, "profiles/work/config.json"), `{"harness":"pi","proxy_enabled":false,"auto_rebuild":true}`)
	put(t, filepath.Join(p.Source, "profiles/work/pi/user directory/data.txt"), "profile payload")
	root := filepath.Join(p.Source, "sessions", name)
	put(t, filepath.Join(root, ".staged-harness/pi/kept folder/file"), "projected payload")
	for _, entry := range []string{"kept folder", "removed folder"} {
		if err := os.Symlink("/devbox/harness-config/"+entry, filepath.Join(root, "pi", entry)); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(filepath.Dir(p.Source), "second workspace")
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"harness":"opencode"}`)
	other := addSession(t, p, workspace, "", "opencode", strings.Repeat("d", 32), true)
	v := inventory(t, p)
	j := mustStage(t, v)
	d := &dockertest.Daemon{Containers: map[string]docker.Container{}, Images: map[string]docker.Image{}, Volumes: map[string]bool{}}
	m := Merger{Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000, Host: map[string]string{"HOME": filepath.Dir(p.Source)}}
	choices := MergeChoices{Projects: []string{"project:" + workspace}, OmitConfig: []string{"session:" + other}, Global: "import"}
	plan, err := m.Plan(t.Context(), j, choices)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"conversion:profile:work", "conversion:session:" + name} {
		found := false
		for _, review := range plan.Reviews {
			if review.Key == key {
				found = true
			}
		}
		if !found {
			t.Fatal("conversion lacks approval", key, plan.Reviews)
		}
	}
	if _, err := m.Apply(t.Context(), p, plan); err == nil {
		t.Fatal("unapproved conversion applied")
	}
	plan = approvedPlan(t, m, j, choices)
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
	if read(t, filepath.Join(p.Destination, "state/installation-id")) != originalID {
		t.Fatal("replaced destination identity")
	}
	if read(t, filepath.Join(p.Destination, "keep.txt")) != "existing Neo data" {
		t.Fatal("lost destination data")
	}
	st := &store.Store{Home: p.Destination, Installation: strings.TrimSpace(originalID)}
	for _, job := range plan.Sessions {
		r, err := st.Read(t.Context(), job.Identity.Name)
		if err != nil || r.ID != v.item(job.Item).SessionID {
			t.Fatal("identity lost", err)
		}
		base := filepath.Join(p.Destination, "sessions", job.Identity.Name, "harnesses")
		if job.Item == "session:"+name {
			for _, f := range []string{"sessions/history.jsonl", "kept folder/file", "user directory/data.txt"} {
				if read(t, filepath.Join(base, "pi/stores/home", f)) == "" {
					t.Fatal("lost Pi data")
				}
			}
			absent(t, filepath.Join(base, "pi/stores/home/removed folder"))
		} else {
			for _, f := range []string{"opencode.db", "opencode.db-wal", "opencode.db-shm"} {
				if read(t, filepath.Join(base, "opencode/stores/data", f)) == "" {
					t.Fatal("lost OpenCode data")
				}
			}
		}
	}
	absent(t, filepath.Join(root, "session.json"))
	if _, err := os.Readlink(filepath.Join(root, "pi/removed folder")); err != nil {
		t.Fatal("source modified", err)
	}
	if !strings.Contains(read(t, filepath.Join(p.Work, "report.txt")), "Omit stale generated") {
		t.Fatal("omission missing from report")
	}
	for _, f := range []string{"journal.json", "report.txt"} {
		text := read(t, filepath.Join(p.Work, f))
		for _, secret := range []string{"metadata-private", "fixture-auth-private", "fixture-conversation-private", "projected payload"} {
			if strings.Contains(text, secret) {
				t.Fatal("private contents exposed")
			}
		}
	}
}

func TestGeneratedProjectionCycleFailsWithoutFollowingForever(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	stage := filepath.Join(root, ".staged-harness/pi/tree")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "pi/tree"), filepath.Join(stage, "loop")} {
		if err := os.Symlink("/devbox/harness-config/tree", path); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Stage(t.Context(), inventory(t, p), Selection{}, &fakeSource{}, nil)
	if err == nil {
		t.Fatal("cyclic projection accepted")
	}
	requireText(t, err.Error(), "cyclic generated config projection")
	absent(t, p.Work)
}

func TestCapturedStaleProjectionRecordsDecision(t *testing.T) {
	stage := t.TempDir()
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	for _, h := range []*tar.Header{
		{Name: "./", Typeflag: tar.TypeDir, Mode: 0700},
		{Name: "./custom", Typeflag: tar.TypeSymlink, Linkname: "/devbox/harness-config/custom"},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var changes []string
	root := t.TempDir()
	if err := extractConfig(t.Context(), tar.NewReader(bytes.NewReader(data.Bytes())), root, stage, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatal("missing capture decision")
	}
	absent(t, filepath.Join(root, "custom"))
	if err := extractConfig(t.Context(), tar.NewReader(bytes.NewReader(data.Bytes())), t.TempDir(), filepath.Join(stage, "absent"), &changes); err == nil {
		t.Fatal("missing entire stage mistaken for stale projection")
	}
}
