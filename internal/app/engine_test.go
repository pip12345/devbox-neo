package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func fixture(t *testing.T) (*Engine, *dockertest.Daemon, Request) {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	s, err := store.Open(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, "profiles", "test", "config.json"), `{"version":1,"harness":"pi"}`)
	write(t, filepath.Join(workspace, ".devbox", "config.json"), `{"version":1}`)
	d := &dockertest.Daemon{}
	sources := []config.Reference{
		{Label: "base", Kind: config.ReferenceFixed, Path: filepath.Join(home, "profiles", "test")},
		{Label: "workspace", Kind: config.ReferenceFixed, Path: filepath.Join(workspace, ".devbox")},
	}
	return &Engine{Store: s, Docker: docker.Runtime{Runner: d}, Streams: docker.Streams{Out: new(bytes.Buffer), Err: new(bytes.Buffer)}, UID: 1000, GID: 1000}, d, Request{Workspace: workspace, LocalName: "test", Sources: sources}
}
func createAndOpen(ctx context.Context, e *Engine, q Request) (Result, error) {
	if result, err := e.Create(ctx, q); err != nil {
		return result, err
	}
	return e.Open(ctx, q)
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func sessionRecord(t *testing.T, e *Engine, name string) store.Record {
	t.Helper()
	var r store.Record
	var err error
	if environment.IsSessionID(name) {
		r, err = e.Store.Find(context.Background(), name, nil)
	} else {
		r, err = e.Store.Read(context.Background(), name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func argvSuffix(args, suffix []string) bool {
	return len(args) >= len(suffix) && slices.Equal(args[len(args)-len(suffix):], suffix)
}
func count(d *dockertest.Daemon, verb string) int {
	n := 0
	for _, c := range d.History() {
		if c[0] == verb {
			n++
		}
	}
	return n
}
func TestCreateReopenDriftAndRecreate(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	first := sessionRecord(t, e, result.SessionID)
	container, _ := sessionSnapshot(t, e, result.SessionID)
	if container.State.Running {
		t.Fatal("automatic shutdown did not stop the container")
	}
	if first.Applied.SetupContainer != container.ID || first.Applied.Fingerprints.Container == "" {
		t.Fatal("creation contract incomplete")
	}
	state := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses/pi/stores/home/sessions/history.json")
	write(t, state, "persistent conversation")
	_, err = e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if count(d, "build") != 2 || count(d, "create") != 1 {
		t.Fatal("reopen recreated runtime")
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	result, err = e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || count(d, "create") != 1 {
		t.Fatal("access must not resolve desired drift or recreate")
	}
	if sessionRecord(t, e, result.SessionID).Applied.Creation.Network != "default" {
		t.Fatal("drift advanced recorded creation settings")
	}
	_, err = e.Recreate(ctx, q, false)
	if err != nil {
		t.Fatal(err)
	}
	second := sessionRecord(t, e, result.SessionID)
	if second.ID != first.ID || second.Applied.ImageID != first.Applied.ImageID || second.Applied.Creation.Network != "host" {
		t.Fatal("recreate did not preserve identity/reuse image/apply settings")
	}
	if count(d, "build") != 2 || count(d, "create") != 2 {
		t.Fatal("unnecessary build or missing recreation")
	}
	if b, err := os.ReadFile(state); err != nil || string(b) != "persistent conversation" {
		t.Fatal("lost persistent state")
	}
	_, err = e.Recreate(ctx, q, true)
	if err != nil {
		t.Fatal(err)
	}
	lastBuild := []string{}
	for _, a := range d.History() {
		if a[0] == "build" {
			lastBuild = a
		}
	}
	if !slices.Contains(lastBuild, "--no-cache") {
		t.Fatal("forced build did not disable cache")
	}
	for _, a := range d.History() {
		if a[0] == "build" {
			for _, v := range a {
				if strings.HasPrefix(v, docker.Namespace+".session=") {
					t.Fatal("image must not carry session ownership")
				}
			}
		}
	}
	third := sessionRecord(t, e, result.SessionID)
	if third.ID != first.ID {
		t.Fatal("rebuild changed durable ID")
	}
}
func TestInvalidDesiredHarnessJSONDoesNotTouchDocker(t *testing.T) {
	e, d, q := fixture(t)
	write(t, filepath.Join(e.Store.Home, "profiles/test/pi/settings.json"), "invalid")
	if _, err := e.Create(context.Background(), q); err == nil || !strings.Contains(err.Error(), "desired settings.json") {
		t.Fatal("invalid desired JSON did not reach validation", err)
	}
	if len(d.History()) != 0 {
		t.Fatal("invalid harness config reached Docker before validation")
	}
}
func TestInvalidConfigDoesNotBlockStoppedOrRunningAccess(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, path, "{broken")
	for _, action := range []string{"open", "exec", "shell", "start", "open", "exec"} {
		if err := accessAction(ctx, e, q, result.SessionID, action); err != nil {
			t.Fatal(action, err)
		}
	}
	if count(d, "build") != 2 || count(d, "create") != 1 {
		t.Fatal("access replaced runtime")
	}
}
func TestRecreateUsesCurrentDefinitionAndSettings(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	first := sessionRecord(t, e, result.SessionID)
	history := filepath.Join(e.Store.Home, "sessions", first.Directory, "harnesses/pi/stores/home/history")
	write(t, history, "preserved")
	if err := e.SetDefault(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, result.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, result.SessionID)
	delete(d.Images, first.Applied.ImageID)
	delete(d.Images, first.Applied.ImageTag)
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"opencode","network":"host"}`)
	write(t, filepath.Join(e.Store.Home, "harnesses/pi/harness.json"), "invalid unselected override")
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	second := sessionRecord(t, e, result.SessionID)
	if first.ID != second.ID || first.Directory != second.Directory || second.Applied.Definition.Name != "opencode" || second.Applied.Creation.Network != "host" || !second.Settings.ManualStart {
		t.Fatal("rebuild did not preserve identity/adopt current configuration")
	}
	if first.Applied.SetupContainer == second.Applied.SetupContainer || count(d, "build") != 4 {
		t.Fatal("missing runtime was not rebuilt")
	}
	if b, err := os.ReadFile(history); err != nil || string(b) != "preserved" {
		t.Fatal("saved history lost", err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.ID != first.ID {
		t.Fatal("default lost", err)
	}
}
func TestManagedConfigWaitsForExplicitApply(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	path := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, path, `{"version":1,"harness":"pi"}`)
	settings := filepath.Join(e.Store.Home, "profiles/test/pi/settings.json")
	write(t, settings, `{"packages":["old"]}`)
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	first := sessionRecord(t, e, result.SessionID)
	live := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses/pi/stores/home/settings.json")
	manifest := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses/pi/managed-config.json")
	before, _ := os.ReadFile(manifest)
	write(t, live, `{"packages":["old"],"theme":"user-owned"}`)
	write(t, settings, `{"packages":["new"]}`)
	result, err = e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatal("access inspected desired config")
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) || sessionRecord(t, e, result.SessionID).Applied.Fingerprints.Runtime != first.Applied.Fingerprints.Runtime {
		t.Fatal("deferral advanced manifest or fingerprint")
	}
	if err = e.Stop(ctx, result.SessionID, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(getFile(t, live)), "new") {
		t.Fatal("stopped access applied desired files")
	}
	if _, err := e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(live)
	if !strings.Contains(string(b), "new") || !strings.Contains(string(b), "user-owned") {
		t.Fatalf("key merge failed: %s", b)
	}
	if count(d, "create") != 1 {
		t.Fatal("runtime sync recreated container")
	}
}
func TestHookChangesRemainPendingUntilExplicitApply(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi"}`)
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses/pi/managed-config.json")
	before, _ := os.ReadFile(manifest)
	write(t, filepath.Join(e.Store.Home, "profiles/test/before-open.sh"), "echo runtime")
	result, err = e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatal("runtime-only hook change incorrectly deferred file synchronization")
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) {
		t.Fatal("running open changed the manifest")
	}
	desired, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	if sessionRecord(t, e, result.SessionID).Applied.Fingerprints.Runtime == desired.Fingerprints.Runtime {
		t.Fatal("open applied changed hooks")
	}
}
func TestOwnershipAndDaemonErrorsFailClosed(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := sessionSnapshot(t, e, result.SessionID)
	c.Config.Labels[docker.Namespace+".installation"] = "foreign"
	d.SetContainer(c)
	before := count(d, "rm")
	if _, err = e.Recreate(ctx, q, false); err == nil {
		t.Fatal("foreign ownership accepted")
	}
	if count(d, "rm") != before {
		t.Fatal("removed foreign container")
	}
	d.Fail = func(a []string) error {
		if a[0] == "container" {
			return errors.New("daemon unavailable")
		}
		return nil
	}
	if _, err = e.Open(ctx, q); err == nil {
		t.Fatal("daemon failure treated as absence")
	}
	if count(d, "create") != 1 {
		t.Fatal("created after inspection error")
	}
}
func TestForegroundStatusAndConcurrentLeases(t *testing.T) {
	e, d, q := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	r := sessionRecord(t, e, result.SessionID)
	launch := append([]string{r.Applied.Launch.Binary}, r.Applied.Launch.Args...)
	d.Attached = func(ctx context.Context, c docker.Command) error {
		if !argvSuffix(c.Args, launch) {
			return nil
		}
		arrived <- struct{}{}
		select {
		case <-release:
			return &docker.ExitError{Code: 23, Operation: "exec"}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Concurrent streams must also be safe; discard output in this test.
	e.Streams = docker.Streams{}
	var errs [2]error
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, err := e.Open(ctx, q); mu.Lock(); errs[i] = err; mu.Unlock() }(i)
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("attached harness did not arrive", ctx.Err())
		}
	}
	if err = e.Stop(ctx, result.SessionID, "", false); err == nil {
		t.Fatal("stop allowed live leases")
	}
	c, _ := sessionSnapshot(t, e, result.SessionID)
	if !c.State.Running {
		t.Fatal("stopped with live commands")
	}
	close(release)
	wg.Wait()
	for _, err := range errs {
		var exit *docker.ExitError
		if !errors.As(err, &exit) || exit.Code != 23 {
			t.Fatalf("lost foreground status: %v", err)
		}
	}
	c, _ = sessionSnapshot(t, e, result.SessionID)
	if c.State.Running {
		t.Fatal("last lease did not stop")
	}
	l, err := e.Store.Lock(ctx, sessionRecord(t, e, result.SessionID).Directory, sessionRecord(t, e, result.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	active, err := l.Active()
	if err != nil || len(active) != 0 {
		t.Fatalf("leaked leases: %v", err)
	}
}
func TestFailedRecordCommitIsNotSuccessfulCreation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var directory string
	d.Fail = func(a []string) error {
		if a[0] == "exec" && slices.Contains(a, "command -v \"$1\" >/dev/null") {
			entries, err := os.ReadDir(filepath.Join(e.Store.Home, "sessions"))
			if err != nil {
				return err
			}
			if len(entries) != 1 {
				return fmt.Errorf("unexpected allocation count")
			}
			directory = entries[0].Name()
			return os.Mkdir(filepath.Join(e.Store.Home, "sessions", directory, "session.json"), 0700)
		}
		return nil
	}
	if _, err := e.Create(ctx, q); err == nil {
		t.Fatal("commit failure hidden")
	}
	if len(d.Containers) != 0 {
		t.Fatal("uncommitted container retained")
	}
	if _, err := e.Store.Read(ctx, directory); err == nil {
		t.Fatal("invalid record treated as valid")
	}
}

func TestMissingImageIsRebuiltOnExplicitRecreate(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, result.SessionID)
	forgetSession(t, e, result.SessionID)
	delete(d.Images, old.Applied.ImageID)
	delete(d.Images, old.Applied.ImageTag)
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	current := sessionRecord(t, e, result.SessionID)
	if current.ID != old.ID || count(d, "build") != 4 {
		t.Fatal("explicit recreation did not rebuild missing image")
	}
}
func TestFailedReplacementKeepsStateAndRetriesCurrentConfiguration(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, result.SessionID)
	write(t, filepath.Join(e.Store.Home, "profiles/test/setup.sh"), "exit 7")
	d.Fail = func(a []string) error {
		if a[0] == "exec" && a[len(a)-1] == "-s" {
			return &docker.ExitError{Code: 7, Operation: "exec"}
		}
		return nil
	}
	if _, err = e.Recreate(ctx, q, false); err == nil {
		t.Fatal("failed setup reported successful recreation")
	}
	after := sessionRecord(t, e, result.SessionID)
	if after.ID != old.ID || after.Applied.Fingerprints != old.Applied.Fingerprints || after.Applied.SetupContainer != old.Applied.SetupContainer {
		t.Fatal("failed replacement advanced durable creation facts")
	}
	d.Fail = nil
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	if sessionRecord(t, e, result.SessionID).Applied.Fingerprints.Container == old.Applied.Fingerprints.Container {
		t.Fatal("retry did not use current setup/config")
	}
}
func TestRecordedValuesNeverPersistHarnessEnv(t *testing.T) {
	e, d, q := fixture(t)
	temporary := ""
	d.Fail = func(a []string) error {
		if a[0] != "create" {
			return nil
		}
		idx := slices.Index(a, "--env-file")
		if idx < 0 {
			return errors.New("missing private env-file transport")
		}
		temporary = a[idx+1]
		info, err := os.Stat(temporary)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0600 {
			return errors.New("env file is not private")
		}
		data, err := os.ReadFile(temporary)
		if err != nil {
			return err
		}
		if string(data) != "API_TOKEN=sentinel-secret\n" {
			return errors.New("wrong env transport content")
		}
		return nil
	}
	def := `{"version":1,"name":"custom","binary":"true","install":{"shell":"","path":[]},"launch":{"args":[],"continue_args":[]},"env":{"API_TOKEN":"sentinel-secret"},"stores":[{"name":"home","scope":"environment","target":"/home/devuser/.custom"}],"config":{"store":"home","path":"."},"session":{},"prepare":[]}`
	write(t, filepath.Join(e.Store.Home, "harnesses/custom/harness.json"), def)
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"custom"}`)
	result, err := createAndOpen(context.Background(), e, q)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("sentinel-secret")) {
		t.Fatal("persisted literal secret")
	}
	for _, args := range d.History() {
		if strings.Contains(strings.Join(args, " "), "sentinel-secret") {
			t.Fatal("secret exposed in argv")
		}
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary env file retained after creation")
	}
	write(t, filepath.Join(e.Store.Home, "harnesses/custom/harness.json"), strings.Replace(def, "sentinel-secret", `multi\nline`, 1))
	before := len(d.History())
	if _, err := e.Recreate(context.Background(), q, false); err == nil {
		t.Fatal("unsupported multiline transport accepted")
	}
	if len(d.History()) != before {
		t.Fatal("multiline transport error reached Docker")
	}
}
func TestRecreateSynchronizesCurrentManagedConfig(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, result.SessionID)
	write(t, filepath.Join(e.Store.Home, "profiles/test/pi/settings.json"), `{"packages":["recovered"]}`)
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses/pi/stores/home/settings.json"))
	if err != nil || !bytes.Contains(b, []byte("recovered")) {
		t.Fatalf("recreation did not synchronize: %s %v", b, err)
	}
}
func TestHookFailureStopsNewlyStartedContainer(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/before-open.sh"), "exit 19")
	d.Fail = func(a []string) error {
		if a[0] == "exec" && argvSuffix(a, []string{"bash", docker.OpenHookPath(environment.Digest([]byte("exit 19")))}) {
			return &docker.ExitError{Code: 19, Operation: "exec"}
		}
		return nil
	}
	result, err := createAndOpen(ctx, e, q)
	var exit *docker.ExitError
	if !errors.As(err, &exit) || exit.Code != 19 {
		t.Fatalf("hook error: %v", err)
	}
	c, exists := sessionSnapshot(t, e, result.SessionID)
	if !exists || c.State.Running {
		t.Fatal("failed hook left a newly started container running")
	}
}
func TestCancellationReleasesLease(t *testing.T) {
	e, d, q := fixture(t)
	result, err := createAndOpen(context.Background(), e, q)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	done := make(chan error, 1)
	d.Attached = func(ctx context.Context, _ docker.Command) error { close(entered); <-ctx.Done(); return ctx.Err() }
	go func() { _, err := e.Open(ctx, q); done <- err }()
	<-entered
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	c, _ := sessionSnapshot(t, e, result.SessionID)
	if c.State.Running {
		t.Fatal("cancellation left container running")
	}
}
func TestFingerprintDependsOnInstallationSalt(t *testing.T) {
	e, _, q := fixture(t)
	s, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	e.Store.Installation, _ = fsutil.ID()
	other, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	if s.Harness.Hash == other.Harness.Hash || s.Fingerprints.Container == other.Fingerprints.Container {
		t.Fatal("unsalted definition/env fingerprints")
	}
	if environment.Compare(s.FingerprintsFor("image-a"), s.FingerprintsFor("image-b")) != environment.Recreate {
		t.Fatal("actual image ID missing from container fingerprint")
	}
}
