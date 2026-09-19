package migration

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func mergeFixture(t *testing.T, fresh bool) (Paths, string, *Journal, Merger, *dockertest.Daemon) {
	t.Helper()
	p, name := fixture(t)
	if fresh {
		if err := os.RemoveAll(p.Destination); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := store.Open(context.Background(), p.Destination); err != nil {
			t.Fatal(err)
		}
	}
	j := mustStage(t, inventory(t, p))
	d := &dockertest.Daemon{Containers: map[string]docker.Container{}, Images: map[string]docker.Image{}, Volumes: map[string]bool{}}
	m := Merger{Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000, Host: map[string]string{"HOME": filepath.Dir(p.Source), "MODEL": "fixture-model", "PASSTHROUGH": "present"}}
	return p, name, j, m, d
}
func approvedPlan(t *testing.T, m Merger, j *Journal, c MergeChoices) MergePlan {
	t.Helper()
	plan, err := m.Plan(context.Background(), j, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan.Reviews {
		if r.Blocking {
			t.Fatalf("blocked %s: %s", r.Item, r.Message)
		}
		c.Accept = append(c.Accept, r.Key)
	}
	plan, err = m.Plan(context.Background(), j, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Ready(); err != nil {
		t.Fatal(err)
	}
	return plan
}
func TestMergeFreshAndExistingUsesNormalRecordsAndFinalPaths(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{true: "fresh", false: "existing"}[fresh], func(t *testing.T) {
			p, name, j, m, d := mergeFixture(t, fresh)
			originalID := ""
			if !fresh {
				originalID = strings.TrimSpace(read(t, filepath.Join(p.Destination, "state/installation-id")))
			}
			plan := approvedPlan(t, m, j, MergeChoices{})
			if len(plan.Sessions) != 1 {
				t.Fatal(plan.Reviews)
			}
			j, err := m.Apply(context.Background(), p, plan)
			if err != nil {
				t.Fatal(err)
			}
			if j.Phase != "completed" {
				t.Fatal(j.Phase)
			}
			id := strings.TrimSpace(read(t, filepath.Join(p.Destination, "state/installation-id")))
			if !fresh && id != originalID {
				t.Fatal("installation identity replaced")
			}
			if id == j.Inventory.Installation {
				t.Fatal("adopted old installation identity")
			}
			st := &store.Store{Home: p.Destination, Installation: id}
			r, err := st.Read(context.Background(), plan.Sessions[0].Identity.Name)
			if err != nil {
				t.Fatal(err)
			}
			if r.ID != j.Inventory.item("session:"+name).SessionID || r.Action != "open" {
				t.Fatal("lost identity/activity")
			}
			b := read(t, filepath.Join(p.Destination, "sessions", r.Identity.Name, "session.json"))
			for _, forbidden := range []string{p.Work, "metadata-private", "fixture-layer-private", "fixture-private-value", "old-alias"} {
				if strings.Contains(b, forbidden) {
					t.Fatalf("record contains %s", forbidden)
				}
			}
			history := filepath.Join(p.Destination, "sessions", r.Identity.Name, "harnesses/pi/stores/home/sessions/history.jsonl")
			if read(t, history) != "fixture-conversation-private" {
				t.Fatal("history missing")
			}
			if d.Containers[r.Identity.Name].State.Running {
				t.Fatal("import left container running")
			}
			if !fresh && read(t, filepath.Join(p.Destination, "keep.txt")) != "existing Neo data" {
				t.Fatal("existing data changed")
			}
			if read(t, filepath.Join(p.Source, "sessions", name, "pi/sessions/history.jsonl")) != "fixture-conversation-private" {
				t.Fatal("source changed")
			}
		})
	}
}
func TestMergeNeedsApprovalAndRechecksDestination(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	plan, err := m.Plan(context.Background(), j, MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("unaccepted changes applied")
	}
	absent(t, filepath.Join(p.Destination, "profiles/work"))
	plan = approvedPlan(t, m, j, MergeChoices{})
	put(t, filepath.Join(p.Destination, "config.json"), `{"version":1,"default_harness":"opencode"}`)
	if _, err = m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("stale approval used")
	}
	absent(t, filepath.Join(p.Destination, "profiles/work"))
}
func TestProfileCollisionRequiresReuseRenameOrSkip(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	put(t, filepath.Join(p.Destination, "profiles/work/config.json"), `{"version":1,"harness":"pi","harness_args":["--existing"]}`)
	before := treeSnapshot(t, filepath.Join(p.Destination, "profiles/work"))
	plan, err := m.Plan(context.Background(), j, MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready() == nil {
		t.Fatal("profile collision silently reused")
	}
	plan = approvedPlan(t, m, j, MergeChoices{Rename: map[string]string{"work": "imported-work"}})
	if plan.Sessions[0].Identity.Profile != "imported-work" {
		t.Fatal("rename did not change slot")
	}
	if _, err = m.Apply(context.Background(), p, plan); err != nil {
		t.Fatal(err)
	}
	if !equalMap(before, treeSnapshot(t, filepath.Join(p.Destination, "profiles/work"))) {
		t.Fatal("existing profile overwritten")
	}
}
func equalMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func TestProjectEditsBackedUpAndResumeAfterPublication(t *testing.T) {
	p, _, _, m, _ := mergeFixture(t, false)
	// Start a separate fixture snapshot including a project-slot session.
	if err := os.RemoveAll(p.Work); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(filepath.Dir(p.Source), "project")
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"version":1,"harness":"pi","host_network":false}`)
	name := addSession(t, p, workspace, "work", "pi", strings.Repeat("c", 32), true)
	j := mustStage(t, inventory(t, p))
	key := "project:" + workspace
	plan := approvedPlan(t, m, j, MergeChoices{Projects: []string{key}})
	original := read(t, filepath.Join(workspace, ".devbox/config.json"))
	failed := false
	m.Fault = func(point string) error {
		if point == "published:"+key && !failed {
			failed = true
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	m.Fault = nil
	j, err := m.Resume(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if j.Merge.Attempts["session:"+name].Phase != "done" {
		t.Fatal("project session missing")
	}
	found := false
	for _, pub := range plan.Publications {
		if pub.Item == key {
			found = true
			if read(t, pub.Backup) != original {
				t.Fatal("project backup wrong")
			}
		}
	}
	if !found {
		t.Fatal("project was not planned")
	}
}
func TestCommittedSessionNeverRecopiedOnResume(t *testing.T) {
	p, _, j, m, d := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "record-committed:") {
			return errors.New("crash after record")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	job := plan.Sessions[0]
	history := filepath.Join(p.Destination, "sessions", job.Identity.Name, "harnesses/pi/stores/home/sessions/history.jsonl")
	put(t, history, "new conversation after commit")
	builds := 0
	for _, args := range d.History() {
		if args[0] == "build" {
			builds++
		}
	}
	m.Fault = nil
	j, err := m.Resume(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, history) != "new conversation after commit" {
		t.Fatal("committed history recopied")
	}
	after := 0
	for _, args := range d.History() {
		if args[0] == "build" {
			after++
		}
	}
	if after != builds {
		t.Fatal("committed session rebuilt")
	}
	if j.Phase != "completed" {
		t.Fatal(j.Phase)
	}
}
func TestPreparedDirectoryIdentityBlocksReplacement(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "session-published:") {
			return errors.New("crash before build")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	root := filepath.Join(p.Destination, "sessions", plan.Sessions[0].Identity.Name)
	if err := os.Rename(root, root+".saved"); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "keep"), "unrelated")
	m.Fault = nil
	if _, err := m.Resume(context.Background(), p); err == nil {
		t.Fatal("adopted replacement directory")
	}
	if read(t, filepath.Join(root, "keep")) != "unrelated" {
		t.Fatal("replacement data lost")
	}
}
func TestRawDockerEnvCannotEnterImportedRecord(t *testing.T) {
	p, _, _, m, _ := mergeFixture(t, false)
	if err := os.RemoveAll(p.Work); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(p.Source, "profiles/work/config.json"), `{"version":1,"harness":"pi","docker_args":["--env=PRIVATE=do-not-save"]}`)
	j := mustStage(t, inventory(t, p))
	plan, err := m.Plan(context.Background(), j, MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range plan.Reviews {
		if strings.HasPrefix(r.Key, "env:") && r.Blocking {
			found = true
		}
	}
	if !found {
		t.Fatal("raw env not blocked")
	}
	if strings.Contains(string(encode(plan)), "do-not-save") {
		t.Fatal("plan leaked raw env")
	}
}
func TestMergeUsesExistingAuthUnlessExplicitlyReplaced(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	put(t, filepath.Join(p.Destination, "auth/pi/auth.json"), `{"token":"existing-private"}`)
	plan := approvedPlan(t, m, j, MergeChoices{})
	if _, err := m.Apply(context.Background(), p, plan); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(p.Destination, "auth/pi/auth.json")) != `{"token":"existing-private"}` {
		t.Fatal("existing auth replaced")
	}
}

type archiveRunner struct {
	daemon  *dockertest.Daemon
	archive []byte
}

func (r archiveRunner) Run(ctx context.Context, c docker.Command) error {
	if len(c.Args) > 2 && c.Args[0] == "cp" && c.Args[2] == "-" {
		_, err := c.Stdout.Write(r.archive)
		return err
	}
	return r.daemon.Run(ctx, c)
}
func archiveConfig(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	if err := w.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0700}); err != nil {
		t.Fatal(err)
	}
	text := `{"custom":"container-private-config"}`
	if err := w.WriteHeader(&tar.Header{Name: "./local-plugin.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(text))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, text); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestOpenCodeCaptureIncludedInImportedConfigStore(t *testing.T) {
	p, _, _, m, d := mergeFixture(t, false)
	if err := os.RemoveAll(p.Work); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(filepath.Dir(p.Source), "oc")
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"version":1,"harness":"opencode"}`)
	name := addSession(t, p, workspace, "", "opencode", strings.Repeat("d", 32), true)
	j := mustStage(t, inventory(t, p))
	c := docker.Container{ID: strings.Repeat("e", 64), Name: "/" + name}
	c.Config.Labels = map[string]string{"devbox.managed": "true", "devbox.installation_id": j.Inventory.Installation, "devbox.session_id": strings.Repeat("d", 32)}
	d.Containers[name] = c
	m.Docker.Runner = archiveRunner{d, archiveConfig(t)}
	var err error
	j, err = m.CaptureConfigs(context.Background(), p, MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	plan := approvedPlan(t, m, j, MergeChoices{Projects: []string{"project:" + workspace}})
	if _, err = m.Apply(context.Background(), p, plan); err != nil {
		t.Fatal(err)
	}
	for _, job := range plan.Sessions {
		if job.Item == "session:"+name {
			path := filepath.Join(p.Destination, "sessions", job.Identity.Name, "harnesses/opencode/stores/config/local-plugin.json")
			if read(t, path) != `{"custom":"container-private-config"}` {
				t.Fatal("container config lost")
			}
		}
	}
	if strings.Contains(read(t, filepath.Join(p.Work, "journal.json")), "container-private-config") {
		t.Fatal("capture leaked into journal")
	}
}
func TestRelocatedCommittedIDIsNotRecreatedAtItsOldSlot(t *testing.T) {
	t.Setenv("MODEL", "fixture-model")
	t.Setenv("PASSTHROUGH", "present")
	p, _, j, m, _ := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "record-committed:") {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	st := &store.Store{Home: p.Destination, Installation: plan.Installation}
	e := &app.Engine{Store: st, Docker: m.Docker, UID: 1000, GID: 1000, Streams: docker.Streams{Out: io.Discard, Err: io.Discard}}
	oldName := plan.Sessions[0].Identity.Name
	if err := e.Stop(context.Background(), oldName, "", false); err != nil {
		t.Fatal(err)
	}
	moved, err := e.Transfer(context.Background(), app.TransferOptions{Mode: "relocate", Source: oldName, Destination: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m.Fault = nil
	if _, err = m.Resume(context.Background(), p); err == nil {
		t.Fatal("recreated a relocated ID")
	}
	absent(t, filepath.Join(p.Destination, "sessions", oldName))
	if _, err = st.Read(context.Background(), moved.Destination); err != nil {
		t.Fatal("relocated record lost", err)
	}
}
func TestChangedPendingInputsRequireNewApproval(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "session-published:") {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	put(t, filepath.Join(p.Destination, "profiles/work/setup.sh"), "#!/bin/bash\n# approved new setup\ntrue\n")
	if err := os.Chmod(filepath.Join(p.Destination, "profiles/work/setup.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	m.Fault = nil
	if _, err := m.Resume(context.Background(), p); err == nil {
		t.Fatal("changed input accepted without review")
	}
	j, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := m.PendingPlan(context.Background(), j, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Ready() == nil {
		t.Fatal("pending change lacks acceptance")
	}
	keys := []string{}
	for _, r := range pending.Reviews {
		keys = append(keys, r.Key)
	}
	pending, err = m.PendingPlan(context.Background(), j, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Reapprove(context.Background(), p, pending); err != nil {
		t.Fatal(err)
	}
	if j, err = m.Resume(context.Background(), p); err != nil || j.Phase != "completed" {
		t.Fatal(err)
	}
}
func TestJournalCannotRedirectPublication(t *testing.T) {
	p, _, j, m, _ := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	m.Fault = func(point string) error {
		if strings.HasPrefix(point, "published:") {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := m.Apply(context.Background(), p, plan); err == nil {
		t.Fatal("fault did not trigger")
	}
	j, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(p.Source), "outside")
	put(t, outside, "keep")
	j.Merge.Plan.Publications[0].Target = outside
	if err = saveJournal(j); err != nil {
		t.Fatal(err)
	}
	m.Fault = nil
	if _, err = m.Resume(context.Background(), p); err == nil {
		t.Fatal("tampered publication accepted")
	}
	if read(t, outside) != "keep" {
		t.Fatal("outside file changed")
	}
}
func TestConfigComparisonWithholdsSensitiveAndUnknownFields(t *testing.T) {
	p, _, j, _, _ := mergeFixture(t, false)
	put(t, filepath.Join(p.Destination, "profiles/work/config.json"), `{"version":1,"extra_env":["KEY=private-env"],"unknown_token":"private-token","harness_args":["private-argument"]}`)
	lines, err := ConfigurationComparison(j, *j.Inventory.item("profile:work"), MergeChoices{})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	for _, secret := range []string{"private-env", "private-token", "private-argument", "fixture-layer-private"} {
		if strings.Contains(text, secret) {
			t.Fatal("comparison leaked", secret)
		}
	}
}
func TestConfigArchiveRejectsTraversalAndEscapingLinks(t *testing.T) {
	for _, header := range []*tar.Header{{Name: "../escape", Typeflag: tar.TypeReg}, {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, {Name: "device", Typeflag: tar.TypeChar}} {
		var data bytes.Buffer
		w := tar.NewWriter(&data)
		if err := w.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0700}); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := extractConfig(context.Background(), tar.NewReader(&data), t.TempDir(), t.TempDir(), nil); err == nil {
			t.Fatal("unsafe archive accepted", header.Name)
		}
	}
}
func TestImportedSessionsWorkWithOrdinaryEngine(t *testing.T) {
	t.Setenv("MODEL", "fixture-model")
	t.Setenv("PASSTHROUGH", "present")
	p, _, j, m, _ := mergeFixture(t, false)
	plan := approvedPlan(t, m, j, MergeChoices{})
	if _, err := m.Apply(context.Background(), p, plan); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{Home: p.Destination, Installation: plan.Installation}
	e := app.Engine{Store: st, Docker: m.Docker, UID: 1000, GID: 1000, Streams: docker.Streams{Out: io.Discard, Err: io.Discard}}
	if _, err := e.Start(context.Background(), plan.Sessions[0].Identity.Name, ""); err != nil {
		t.Fatal(err)
	}
}
