package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"golang.org/x/sys/unix"
)

type fakeSource struct {
	calls int
	check func(int) error
}

func (f *fakeSource) CheckIdle(_ context.Context, _ *Inventory, _ map[string]string) error {
	f.calls++
	if f.check != nil {
		return f.check(f.calls)
	}
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
func fixtureFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func fixture(t *testing.T) (Paths, string) {
	t.Helper()
	root := t.TempDir()
	p, err := NewPaths(filepath.Join(root, "old"), filepath.Join(root, "neo"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(p.Source, "state/installation-id"), strings.Repeat("a", 32)+"\n")
	put(t, filepath.Join(p.Source, "global.json"), fixtureFile(t, "global-v2.json"))
	put(t, filepath.Join(p.Source, "profiles/work/config.json"), fixtureFile(t, "layer-v1.json"))
	put(t, filepath.Join(p.Source, "profiles/work/Dockerfile"), "FROM debian:bookworm-slim\nCOPY inputs /inputs\n")
	put(t, filepath.Join(p.Source, "profiles/work/inputs/build.txt"), "build context")
	script := filepath.Join(p.Source, "profiles/work/setup.sh")
	put(t, script, "#!/bin/bash\ntrue\n")
	if err := os.Chmod(script, 0755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(p.Source, "auth/pi/auth.json"), `{"token":"fixture-auth-private"}`)
	workspace := filepath.Join(root, "api")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	name := addSession(t, p, workspace, "work", "pi", strings.Repeat("b", 32), false)
	put(t, filepath.Join(p.Destination, "keep.txt"), "existing Neo data")
	return p, name
}
func addSession(t *testing.T, p Paths, workspace, profile, harness, id string, project bool) string {
	t.Helper()
	suffix := profile
	if project {
		suffix = ".project"
	}
	name := oldContainerName(workspace, suffix)
	root := filepath.Join(p.Source, "sessions", name)
	put(t, filepath.Join(root, "session.json"), fmt.Sprintf(`{"version":1,"id":%q,"alias":"old-alias","created_at":"2025-01-02T03:04:05Z","last_activity_at":"2025-02-03T04:05:06Z","last_action":"open","cloned_from":"ancestor","relocated_from":["old-name"]}`, id))
	put(t, filepath.Join(root, "metadata.json"), fmt.Sprintf(`{"metadata_version":4,"ownership_version":1,"name":%q,"folder":%q,"profile":%q,"harness":%q,"proxy_enabled":true,"host_network":false,"read_only":false,"extra_env":["SECRET=metadata-private"],"creation_settings":{"profile":%q,"harness":%q,"extra_env":["SECRET=metadata-private"],"proxy":{"enabled":true}},"created_at":"2025-01-02T03:04:05Z"}`, name, workspace, profile, harness, profile, harness))
	state := "sessions/history.jsonl"
	if harness == "opencode" {
		state = "opencode.db"
	}
	put(t, filepath.Join(root, harness, state), "fixture-conversation-private")
	if harness == "opencode" {
		put(t, filepath.Join(root, harness, "opencode.db-wal"), "wal")
		put(t, filepath.Join(root, harness, "opencode.db-shm"), "shm")
	}
	put(t, filepath.Join(root, harness, "auth.json"), "overlay-not-history")
	return name
}
func inventory(t *testing.T, p Paths) *Inventory {
	t.Helper()
	v, err := InventorySource(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func mustStage(t *testing.T, v *Inventory) *Journal {
	t.Helper()
	j, err := Stage(context.Background(), v, Selection{}, &fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected path %s: %v", path, err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[rel] = digest(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestInventoryIsReadOnlyAndReportRedactsValues(t *testing.T) {
	p, name := fixture(t)
	before := treeSnapshot(t, p.Source)
	v := inventory(t, p)
	if got := treeSnapshot(t, p.Source); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Fatal("inventory changed source")
	}
	absent(t, p.Work)
	absent(t, filepath.Join(p.Source, "state/locks"))
	var out bytes.Buffer
	if err := Report(&out, v, nil); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-private-value", "fixture-layer-private", "fixture-auth-private", "metadata-private", "fixture-conversation-private", "overlay-not-history"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("report leaked %s", secret)
		}
	}
	for _, text := range []string{name, "old-alias", "ancestor", "old-name", "Recorded creation settings", "Proxy", "Explicit merge approval"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("report missing %s", text)
		}
	}
	if item := v.item("session:" + name); item == nil || item.SessionID != strings.Repeat("b", 32) || item.Target != environment.ContainerName(item.Workspace, "profile:work") {
		t.Fatalf("bad session: %+v", item)
	}
}
func TestStageConvertsAndCopiesWithoutTouchingExistingNeo(t *testing.T) {
	p, name := fixture(t)
	v := inventory(t, p)
	before := treeSnapshot(t, p.Destination)
	j := mustStage(t, v)
	if j.Phase != "prepared" {
		t.Fatal(j.Phase)
	}
	if fmt.Sprint(before) != fmt.Sprint(treeSnapshot(t, p.Destination)) {
		t.Fatal("changed existing Neo")
	}
	root := filepath.Join(p.Work, "staged-home")
	var global config.Global
	if config.Decode([]byte(read(t, filepath.Join(root, "config.json"))), &global) != nil || global.Version != 1 {
		t.Fatal("global not converted")
	}
	layer, err := config.ParseLayer([]byte(read(t, filepath.Join(root, "profiles/work/config.json"))), false)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Network == nil || *layer.Network != "default" || layer.HarnessArgs[1] != "${env:MODEL}" {
		t.Fatalf("lost source settings: %+v", layer)
	}
	if read(t, filepath.Join(root, "profiles/work/inputs/build.txt")) != "build context" {
		t.Fatal("lost build input")
	}
	info, err := os.Stat(filepath.Join(root, "profiles/work/setup.sh"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("lost executable/private mode")
	}
	target := v.item("session:" + name).Target
	history := filepath.Join(root, "sessions", target, "harnesses/pi/stores/home/sessions/history.jsonl")
	if read(t, history) != "fixture-conversation-private" {
		t.Fatal("lost history")
	}
	absent(t, filepath.Join(root, "sessions", target, "session.json"))
	absent(t, filepath.Join(root, "sessions", target, "harnesses/pi/stores/home/auth.json"))
	if read(t, filepath.Join(root, "auth/pi/auth.json")) != `{"token":"fixture-auth-private"}` {
		t.Fatal("lost auth")
	}
	for _, path := range []string{"journal.json", "report.txt"} {
		info, err := os.Stat(filepath.Join(p.Work, path))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("report/journal not private")
		}
		if strings.Contains(read(t, filepath.Join(p.Work, path)), "fixture-private-value") {
			t.Fatal("leaked config values")
		}
	}
	put(t, history, "new independent data")
	if read(t, filepath.Join(p.Source, "sessions", name, "pi/sessions/history.jsonl")) != "fixture-conversation-private" {
		t.Fatal("copy shares source writes")
	}
}
func TestUnsupportedOwnersRequireExplicitSkipAndPropagate(t *testing.T) {
	p, _ := fixture(t)
	put(t, filepath.Join(p.Source, "profiles/unsupported/config.json"), `{"version":1,"harness":"codex"}`)
	workspace := filepath.Join(filepath.Dir(p.Source), "other")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	name := addSession(t, p, workspace, "unsupported", "pi", strings.Repeat("c", 32), false)
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"version":1,"harness":"claude"}`)
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("silently skipped unsupported owner")
	}
	project := "project:" + workspace
	if len(v.item(project).Issues) == 0 {
		t.Fatal("unsupported project not discovered")
	}
	excluded, err := v.Select(Selection{Skip: []string{"profile:unsupported", project}})
	if err != nil {
		t.Fatal(err)
	}
	if excluded["session:"+name] == "" {
		t.Fatal("dependency not excluded")
	}
	j, err := Stage(context.Background(), v, Selection{Skip: []string{"profile:unsupported", project}}, &fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(p.Work, "report.txt")), "dependency skipped") {
		t.Fatal("report hides exclusion")
	}
	if j.Excluded["profile:unsupported"] == "" {
		t.Fatal("lost skip")
	}
	absent(t, filepath.Join(p.Work, "staged-home/profiles/unsupported"))
}
func TestProjectSlotDoesNotBecomeInheritedProfileSlot(t *testing.T) {
	p, _ := fixture(t)
	workspace := filepath.Join(filepath.Dir(p.Source), "project")
	put(t, filepath.Join(workspace, ".devbox/config.json"), `{"version":1,"harness":"opencode","host_network":false}`)
	name := addSession(t, p, workspace, "work", "opencode", strings.Repeat("d", 32), true)
	before := treeSnapshot(t, workspace)
	v := inventory(t, p)
	item := v.item("session:" + name)
	if item.Profile != "" || item.SourceProfile != "work" || item.Target != environment.ContainerName(workspace, "project") {
		t.Fatalf("wrong slot: %+v", item)
	}
	mustStage(t, v)
	if fmt.Sprint(before) != fmt.Sprint(treeSnapshot(t, workspace)) {
		t.Fatal("staging edited project")
	}
	for _, file := range []string{"opencode.db", "opencode.db-wal", "opencode.db-shm"} {
		if read(t, filepath.Join(p.Work, "staged-home/sessions", item.Target, "harnesses/opencode/stores/data", file)) == "" {
			t.Fatal("lost database companion")
		}
	}
	if !strings.Contains(read(t, filepath.Join(p.Work, "report.txt")), "container-only") {
		t.Fatal("incomplete config capture not disclosed")
	}
}
func TestSourceVersionAndDuplicateJSONFailClosed(t *testing.T) {
	for _, data := range []string{`{"version":1}`, `{"version":2,"version":2}`, `{"version":2,"secret-extra":"never-print-this"}`} {
		t.Run(data, func(t *testing.T) {
			p, _ := fixture(t)
			put(t, filepath.Join(p.Source, "global.json"), data)
			_, err := InventorySource(context.Background(), p)
			if err == nil {
				t.Fatal("accepted invalid schema")
			}
			if strings.Contains(err.Error(), "never-print-this") {
				t.Fatal("leaked schema contents")
			}
			absent(t, p.Work)
		})
	}
}
func TestPendingRelocationAndNameMismatchAreItemErrors(t *testing.T) {
	p, name := fixture(t)
	path := filepath.Join(p.Source, "sessions", name, "session.json")
	var r map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &r); err != nil {
		t.Fatal(err)
	}
	r["pending_relocation"] = map[string]string{"destination": "elsewhere"}
	put(t, path, string(encode(r)))
	v := inventory(t, p)
	if !strings.Contains(strings.Join(v.item("session:"+name).Issues, " "), "Pending relocation") {
		t.Fatal("pending transfer accepted")
	}
	wrong := "devbox-wrong-12345678-work"
	if err := os.Rename(filepath.Join(p.Source, "sessions", name), filepath.Join(p.Source, "sessions", wrong)); err != nil {
		t.Fatal(err)
	}
	if len(inventory(t, p).item("session:"+wrong).Issues) == 0 {
		t.Fatal("identity mismatch accepted")
	}
}
func TestExternalAuthRequiresApprovalAndNeverMovesOriginal(t *testing.T) {
	p, _ := fixture(t)
	external := filepath.Join(filepath.Dir(p.Source), "external-auth.json")
	put(t, external, `{"token":"external-private"}`)
	path := filepath.Join(p.Source, "auth/pi/auth.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	if _, err := v.Select(Selection{}); err == nil {
		t.Fatal("external auth copied without consent")
	}
	if _, err := Stage(context.Background(), v, Selection{ExternalAuth: []string{"auth:pi"}}, &fakeSource{}); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(p.Work, "staged-home/auth/pi/auth.json"), "changed")
	if read(t, external) != `{"token":"external-private"}` {
		t.Fatal("external original changed")
	}
}
func TestGeneratedConfigAndInternalSymlink(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name)
	put(t, filepath.Join(root, ".staged-harness/pi/skills/example/SKILL.md"), "managed skill")
	if err := os.Symlink("/devbox/harness-config/skills", filepath.Join(root, "pi/skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sessions/history.jsonl", filepath.Join(root, "pi/history-link")); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	if len(v.item("session:"+name).Issues) > 0 {
		t.Fatal(v.item("session:" + name).Issues)
	}
	mustStage(t, v)
	target := filepath.Join(p.Work, "staged-home/sessions", v.item("session:"+name).Target, "harnesses/pi/stores/home")
	if read(t, filepath.Join(target, "skills/example/SKILL.md")) != "managed skill" {
		t.Fatal("generated link not materialized")
	}
	link, err := os.Readlink(filepath.Join(target, "history-link"))
	if err != nil || link != "sessions/history.jsonl" {
		t.Fatal("internal link changed")
	}
}
func TestEscapingLinksAndSpecialFilesBlockAffectedOwner(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name, "pi")
	outside := filepath.Join(filepath.Dir(p.Source), "outside")
	put(t, outside, "outside-private")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	v := inventory(t, p)
	if len(v.item("session:"+name).Issues) == 0 {
		t.Fatal("escaping link accepted")
	}
	if _, err := Stage(context.Background(), v, Selection{}, &fakeSource{}); err == nil {
		t.Fatal("staged unsafe tree")
	}
	absent(t, p.Work)
}
func TestInterruptedStagingResumesAndRecognizesPublishedFiles(t *testing.T) {
	p, _ := fixture(t)
	v := inventory(t, p)
	failure := errors.New("inspection lost")
	j, err := Stage(context.Background(), v, Selection{}, &fakeSource{check: func(n int) error {
		if n == 2 {
			return failure
		}
		return nil
	}})
	if !errors.Is(err, failure) || j == nil || j.Phase != "staging" {
		t.Fatalf("not resumable: %+v %v", j, err)
	}
	// Simulate publication completing just before its bookkeeping write.
	j.Completed = map[string]string{}
	if err := saveJournal(j); err != nil {
		t.Fatal(err)
	}
	j, err = ResumeStage(context.Background(), p, &fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if j.Phase != "prepared" {
		t.Fatal(j.Phase)
	}
	if _, err = ResumeStage(context.Background(), p, &fakeSource{}); err == nil || !strings.Contains(err.Error(), "does not authorize merge") {
		t.Fatal("resume escalated scope")
	}
}
func TestResumeNeverOverwritesEditedStagingOrChangedSource(t *testing.T) {
	for _, changeSource := range []bool{false, true} {
		t.Run(fmt.Sprint(changeSource), func(t *testing.T) {
			p, _ := fixture(t)
			v := inventory(t, p)
			_, err := Stage(context.Background(), v, Selection{}, &fakeSource{check: func(n int) error {
				if n == 2 {
					return errors.New("stop")
				}
				return nil
			}})
			if err == nil {
				t.Fatal("expected interruption")
			}
			path := filepath.Join(p.Work, "staged-home/profiles/work/inputs/build.txt")
			if changeSource {
				path = filepath.Join(p.Source, "profiles/work/inputs/build.txt")
			}
			put(t, path, "later edit")
			if _, err := ResumeStage(context.Background(), p, &fakeSource{}); err == nil {
				t.Fatal("silently overwrote changed input")
			}
			if read(t, path) != "later edit" {
				t.Fatal("lost edit")
			}
		})
	}
}
func TestSourceAdditionsAndRunningWriterRefuseBeforeWorkCreation(t *testing.T) {
	p, name := fixture(t)
	v := inventory(t, p)
	put(t, filepath.Join(p.Source, "sessions", name, "pi/sessions/new.jsonl"), "new")
	if _, err := Stage(context.Background(), v, Selection{}, &fakeSource{}); err == nil {
		t.Fatal("missed new conversation")
	}
	absent(t, p.Work)
	v = inventory(t, p)
	if _, err := Stage(context.Background(), v, Selection{}, &fakeSource{check: func(int) error { return errors.New("running") }}); err == nil {
		t.Fatal("copied live state")
	}
	absent(t, p.Work)
	put(t, filepath.Join(p.Source, "sessions", name, ".active/test.json"), fmt.Sprintf(`{"pid":%d,"started_at":"2025-01-01T00:00:00Z"}`, os.Getpid()))
	v = inventory(t, p)
	if _, err := Stage(context.Background(), v, Selection{}, &fakeSource{}); err == nil {
		t.Fatal("copied active lease")
	}
	absent(t, p.Work)
}
func TestExistingWorkAndPathAliasesAreNeverOverwritten(t *testing.T) {
	p, _ := fixture(t)
	put(t, filepath.Join(p.Work, "keep"), "unrelated")
	if _, err := Stage(context.Background(), inventory(t, p), Selection{}, &fakeSource{}); err == nil {
		t.Fatal("overwrote unrelated work")
	}
	if read(t, filepath.Join(p.Work, "keep")) != "unrelated" {
		t.Fatal("work lost")
	}
	for _, destination := range []string{p.Source, filepath.Join(p.Source, "nested"), filepath.Dir(p.Source)} {
		if _, err := NewPaths(p.Source, destination); err == nil {
			t.Fatal("overlap accepted")
		}
	}
	link := filepath.Join(filepath.Dir(p.Source), "alias")
	if err := os.Symlink(p.Source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPaths(link, p.Destination); err == nil {
		t.Fatal("symlink alias accepted")
	}
}
func TestDockerSourceChecksOwnershipAndUnselectedSharedWriters(t *testing.T) {
	p, name := fixture(t)
	v := inventory(t, p)
	c := docker.Container{ID: strings.Repeat("e", 64), Name: "/" + name}
	c.Config.Labels = map[string]string{"devbox.managed": "true", "devbox.installation_id": v.Installation, "devbox.session_id": strings.Repeat("b", 32)}
	d := &dockertest.Daemon{Containers: map[string]docker.Container{name: c}}
	runtime := DockerSource{docker.Runtime{Runner: d}}
	if err := runtime.CheckIdle(context.Background(), v, nil); err != nil {
		t.Fatal(err)
	}
	c.State.Running = true
	d.Containers[name] = c
	if err := runtime.CheckIdle(context.Background(), v, map[string]string{"session:" + name: "skipped"}); err == nil {
		t.Fatal("skipped container can write shared auth")
	}
	c.State.Running = false
	c.Config.Labels["devbox.session_id"] = "wrong"
	d.Containers[name] = c
	if err := runtime.CheckIdle(context.Background(), v, nil); err == nil {
		t.Fatal("ownership mismatch accepted")
	}
	for _, args := range d.History() {
		if len(args) < 2 || args[0] != "container" || (args[1] != "ls" && args[1] != "inspect") {
			t.Fatalf("mutating Docker operation: %v", args)
		}
	}
}
func TestResumeRejectsUnknownWorkWithoutCreatingALock(t *testing.T) {
	p, _ := fixture(t)
	put(t, filepath.Join(p.Work, "journal.json"), `{"version":999}`)
	before := treeSnapshot(t, p.Work)
	if _, err := ResumeStage(context.Background(), p, &fakeSource{}); err == nil {
		t.Fatal("accepted unknown work")
	}
	if fmt.Sprint(before) != fmt.Sprint(treeSnapshot(t, p.Work)) {
		t.Fatal("resume modified unrelated work")
	}
	notPath := filepath.Join(p.Work, "lock")
	absent(t, notPath)
}
func TestSourceLockOrderMatchesOldRuntimeAndCancellationReleases(t *testing.T) {
	p, _ := fixture(t)
	v := inventory(t, p)
	v.Items = append(v.Items, Item{Kind: "session", Name: "devbox-aaa", Path: filepath.Join(p.Source, "sessions/devbox-aaa")}, Item{Kind: "session", Name: "devbox-zzz", Path: filepath.Join(p.Source, "sessions/devbox-zzz")})
	dir, err := fsutil.Dir(p.Source, "state/locks/sessions", 0700)
	if err != nil {
		t.Fatal(err)
	}
	paths := sourceLockPaths(v, dir)
	first := sha256.Sum256([]byte("devbox-aaa"))
	if paths[0] != filepath.Join(dir, hex.EncodeToString(first[:16])+".operation.lock") {
		t.Fatal("locks ordered by hash instead of source name")
	}
	for n, path := range paths {
		kind := "operation"
		if n >= len(paths)/2 {
			kind = "record"
		}
		if !strings.HasSuffix(path, "."+kind+".lock") {
			t.Fatal("record lock interleaved with operations")
		}
	}
	held, err := fsutil.Lock(context.Background(), paths[1])
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = lockSource(ctx, v)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err = fsutil.Unlock(held); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	release, err := lockSource(ctx2, v)
	if err != nil {
		t.Fatal("partial locks leaked", err)
	}
	release()
}
func TestFIFOsAndLinksToExcludedAuthAreBlocked(t *testing.T) {
	p, name := fixture(t)
	root := filepath.Join(p.Source, "sessions", name, "pi")
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if len(inventory(t, p).item("session:"+name).Issues) == 0 {
		t.Fatal("FIFO accepted")
	}
	if err := os.Remove(filepath.Join(root, "pipe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("auth.json", filepath.Join(root, "auth-alias")); err != nil {
		t.Fatal(err)
	}
	if len(inventory(t, p).item("session:"+name).Issues) == 0 {
		t.Fatal("excluded auth link accepted")
	}
}
func TestReportNeverLeaksAnUnknownRuntimeError(t *testing.T) {
	p, _ := fixture(t)
	v := inventory(t, p)
	cause := errors.New("private-error-value")
	_, err := Stage(context.Background(), v, Selection{}, &fakeSource{check: func(n int) error {
		if n == 2 {
			return cause
		}
		return nil
	}})
	if !errors.Is(err, cause) {
		t.Fatal("lost cause", err)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("public error leaked private cause")
	}
	if strings.Contains(read(t, filepath.Join(p.Work, "report.txt")), cause.Error()) {
		t.Fatal("report leaked private cause")
	}
	if strings.Contains(read(t, filepath.Join(p.Work, "journal.json")), cause.Error()) {
		t.Fatal("journal leaked private cause")
	}
}
func TestCachesAreOptionalAndUnknownSkipIsAnError(t *testing.T) {
	p, _ := fixture(t)
	put(t, filepath.Join(p.Source, "cache/harnesses/pi/npm-cache/package"), "cached")
	v := inventory(t, p)
	excluded, err := v.Select(Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if excluded["cache:pi/npm-cache"] == "" {
		t.Fatal("cache selected by default")
	}
	if _, err := v.Select(Selection{Skip: []string{"typo"}}); err == nil {
		t.Fatal("unknown skip ignored")
	}
	_, err = Stage(context.Background(), v, Selection{Caches: true}, &fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(p.Work, "staged-home/cache/harnesses/pi/npm-cache/package")) != "cached" {
		t.Fatal("cache omitted")
	}
}
