package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

func fixture(t *testing.T) Service {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Service{Home: s.Home}
}
func put(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func get(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestProjectHintsKeepEnteredFolder(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	workspace := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entered, err := filepath.Rel(cwd, workspace)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Project(entered)
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != entered || o.Workspace != workspace || o.Root != filepath.Join(workspace, ".devbox") {
		t.Fatal("hint spelling changed filesystem identity", o)
	}
	created, err := s.Create(ctx, o, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(created.Next[0].Command, " "); got != "devbox-neo project init "+entered {
		t.Fatal(got)
	}
	initialized, err := s.Init(ctx, o, InitOptions{Harness: "pi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(initialized.Next) != 1 || strings.Join(initialized.Next[0].Command, " ") != "devbox-neo create "+entered {
		t.Fatal(initialized.Next)
	}
}

func TestCreateAndInitAreSeparateAndIdempotent(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	o, err := s.Profile("basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Init(ctx, o, InitOptions{Harness: "pi"}); err == nil {
		t.Fatal("init created owner implicitly")
	}
	if _, err = s.Create(ctx, o, ""); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(get(t, filepath.Join(o.Root, "config.json")), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields["version"] != float64(1) {
		t.Fatal("create was not sparse")
	}
	if _, err = s.Init(ctx, o, InitOptions{}); err == nil {
		t.Fatal("unconfigured noninteractive init guessed a harness")
	}
	options := InitOptions{Harness: "pi", Artifacts: []string{"harness-config", "setup.sh"}}
	result, err := s.Init(ctx, o, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 2 {
		t.Fatal(result)
	}
	if len(result.Next) != 2 || strings.Join(result.Next[0].Command, " ") != "devbox-neo create <folder>" || result.Next[1].Reason != "Use this profile as default (optional)" {
		t.Fatalf("wrong profile creation guidance: %v", result.Next)
	}
	p := filepath.Join(o.Root, "pi/settings.json")
	put(t, p, "user-owned settings")
	before := get(t, filepath.Join(o.Root, "config.json"))
	result, err = s.Init(ctx, o, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 0 || len(result.Skipped) != 2 || string(get(t, p)) != "user-owned settings" {
		t.Fatal("init overwrote source artifacts")
	}
	if !bytes.Equal(before, get(t, filepath.Join(o.Root, "config.json"))) {
		t.Fatal("idempotent init rewrote config")
	}
	if _, err = s.Create(ctx, o, ""); err == nil {
		t.Fatal("create overwrote existing owner")
	}
	g, _, err := s.global()
	if err != nil || g.DefaultHarness != "" || g.DefaultProfile != "" {
		t.Fatal("create/init selected global defaults")
	}
}
func TestConcurrentCreatePublishesOneCompleteOwner(t *testing.T) {
	s := fixture(t)
	o, _ := s.Profile("same")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Create(context.Background(), o, ""); results <- err }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("creation was not exclusive")
	}
	if _, _, err := readLayer(o); err != nil {
		t.Fatal(err)
	}
	empty, _ := s.Profile("empty")
	if err := os.Mkdir(empty.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(context.Background(), empty, ""); err == nil {
		t.Fatal("existing empty directory replaced")
	}
}
func TestStandaloneCopyPreservesSourceAndExcludesProfile(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	source, _ := s.Profile("base")
	put(t, filepath.Join(source.Root, "config.json"), `{"version":1,"harness":"pi","harness_args":["--from-source"]}`)
	put(t, filepath.Join(source.Root, "setup.sh"), "echo source")
	put(t, filepath.Join(source.Root, "pi/nested/file.txt"), "source config")
	if err := s.SetDefault(ctx, "base"); err != nil {
		t.Fatal(err)
	}
	project, _ := s.Project(t.TempDir())
	result, err := s.Create(ctx, project, "base")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 3 {
		t.Fatal(result)
	}
	resolved, err := artifact.Resolve(s.Home, project.Workspace, "", config.Layer{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Profile != "" || len(resolved.Settings.HarnessArgs) != 1 {
		t.Fatal("copied profile was applied twice")
	}
	put(t, filepath.Join(source.Root, "pi/nested/file.txt"), "changed source")
	if string(get(t, filepath.Join(project.Root, "pi/nested/file.txt"))) != "source config" {
		t.Fatal("copy retained linkage")
	}
	if _, err = s.Create(ctx, project, "base"); err == nil {
		t.Fatal("copy overwrote existing project")
	}
}
func TestSourceEditsPreserveExpressionsWithoutReadingHostValues(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	source, _ := s.Profile("expressions")
	raw := `{"version":1,"extra_env":["TOKEN=${env:NOT_READ}"],"harness":"pi"}`
	put(t, filepath.Join(source.Root, "config.json"), raw)
	project, _ := s.Project(t.TempDir())
	if _, err := s.Create(ctx, project, "expressions"); err != nil {
		t.Fatal(err)
	}
	b := get(t, filepath.Join(project.Root, "config.json"))
	if !bytes.Contains(b, []byte("${env:NOT_READ}")) {
		t.Fatal("source expression was flattened")
	}
	if string(get(t, filepath.Join(source.Root, "config.json"))) != raw {
		t.Fatal("source was modified")
	}
	put(t, filepath.Join(s.Home, "config.json"), `{"version":1,"global_env":["X=${env:NOT_READ}"]}`)
	if err := s.SetDefault(ctx, "expressions"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(get(t, filepath.Join(s.Home, "config.json")), []byte("${env:NOT_READ}")) {
		t.Fatal("unrelated global expression changed")
	}
}
func TestInvalidCopyAndInitDoNotPublishChanges(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	source, _ := s.Profile("broken")
	put(t, filepath.Join(source.Root, "config.json"), `{"version":1,"inherit_profile":false}`)
	project, _ := s.Project(t.TempDir())
	if _, err := s.Create(ctx, project, "broken"); err == nil {
		t.Fatal("invalid source copied")
	}
	if _, err := os.Lstat(project.Root); !os.IsNotExist(err) {
		t.Fatal("failed copy published a destination")
	}
	put(t, filepath.Join(source.Root, "config.json"), `{"version":1}`)
	put(t, filepath.Join(source.Root, "pi/regular"), "safe")
	if err := os.Symlink("pi", filepath.Join(source.Root, "opencode")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, project, "broken"); err == nil {
		t.Fatal("symlink config root copied")
	}
	before := get(t, filepath.Join(source.Root, "config.json"))
	for _, options := range []InitOptions{{Harness: "missing"}, {Harness: "pi", Artifacts: []string{"../escape"}}, {Harness: "inherit"}} {
		if _, err := s.Init(ctx, source, options); err == nil {
			t.Fatal("invalid init accepted")
		}
		if !bytes.Equal(before, get(t, filepath.Join(source.Root, "config.json"))) {
			t.Fatal("invalid init changed config")
		}
	}
}
func TestProjectInitInheritanceUsesResolver(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		t.Run(map[bool]string{false: "profile", true: "global only"}[standalone], func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			source, _ := s.Profile("base")
			put(t, filepath.Join(source.Root, "config.json"), `{"version":1,"harness":"opencode"}`)
			put(t, filepath.Join(s.Home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi"}`)
			project, _ := s.Project(t.TempDir())
			if _, err := s.Create(ctx, project, ""); err != nil {
				t.Fatal(err)
			}
			raw := `{"version":1,"harness":"pi"}`
			if standalone {
				raw = `{"version":1,"harness":"opencode","inherit_profile":false}`
			}
			put(t, filepath.Join(project.Root, "config.json"), raw)
			result, err := s.Init(ctx, project, InitOptions{Harness: "inherit"})
			if err != nil {
				t.Fatal(err)
			}
			want := "opencode"
			if standalone {
				want = "pi"
			}
			if result.Harness != want {
				t.Fatal(result)
			}
			if len(result.Next) != 1 || strings.Join(result.Next[0].Command, " ") != "devbox-neo create "+project.Workspace {
				t.Fatalf("wrong project creation guidance: %v", result.Next)
			}
			_, layer, err := readLayer(project)
			if err != nil || layer.Harness != nil {
				t.Fatal("inheritance was flattened to an explicit harness")
			}
		})
	}
	s := fixture(t)
	p, _ := s.Project(t.TempDir())
	s.Create(context.Background(), p, "")
	before := get(t, filepath.Join(p.Root, "config.json"))
	if _, err := s.Init(context.Background(), p, InitOptions{Harness: "inherit"}); err == nil {
		t.Fatal("empty inheritance accepted")
	}
	if !bytes.Equal(before, get(t, filepath.Join(p.Root, "config.json"))) {
		t.Fatal("failed inheritance changed config")
	}
}
func TestProfileListShowsInvalidEntriesAndSetIsExplicit(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	good, _ := s.Profile("good")
	s.Create(ctx, good, "")
	bad, _ := s.Profile("bad")
	put(t, filepath.Join(bad.Root, "config.json"), "broken")
	if err := s.SetDefault(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Error == "" || !entries[1].Default {
		t.Fatal(entries)
	}
	if err = s.SetDefault(ctx, "bad"); err == nil {
		t.Fatal("invalid default selected")
	}
	if err = s.SetDefault(ctx, ""); err != nil {
		t.Fatal(err)
	}
	g, _, _ := s.global()
	if g.DefaultProfile != "" {
		t.Fatal("default not cleared")
	}
}
func TestInitKeepsConfiguredHarnessAndUsesExplicitChoices(t *testing.T) {
	s := fixture(t)
	o, _ := s.Profile("choices")
	s.Create(context.Background(), o, "")
	options := InitOptions{ChooseHarness: func(choices []string, _ []harness.Issue) (string, error) { return "opencode", nil }, ChooseArtifacts: func(choices []string) ([]string, error) { return []string{"harness-config"}, nil }}
	result, err := s.Init(context.Background(), o, options)
	if err != nil || result.Harness != "opencode" {
		t.Fatal(result, err)
	}
	options.ChooseHarness = func([]string, []harness.Issue) (string, error) {
		t.Fatal("configured harness prompted again")
		return "", nil
	}
	if _, err = s.Init(context.Background(), o, options); err != nil {
		t.Fatal(err)
	}
}
func TestProfileDeleteOnlyRemovesItsConfiguration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	o, _ := s.Profile("remove")
	s.Create(ctx, o, "")
	if err := s.SetDefault(ctx, "remove"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(s.Home, "sessions/retained-marker")
	put(t, marker, "retained")
	if _, err := s.DeleteProfile(ctx, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.Root); !os.IsNotExist(err) {
		t.Fatal("profile remained")
	}
	if string(get(t, marker)) != "retained" {
		t.Fatal("profile deletion changed session state")
	}
	global, _, err := s.global()
	if err != nil || global.DefaultProfile != "remove" {
		t.Fatal("profile deletion changed global configuration", err)
	}
}

func TestCopyIncludesActiveBuildContextAndInitNeverOverwritesDockerfiles(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	source, _ := s.Profile("image")
	s.Create(ctx, source, "")
	if _, err := s.Init(ctx, source, InitOptions{Harness: "pi", Artifacts: []string{"Dockerfile"}}); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(source.Root, "Dockerfile"), "FROM debian:bookworm-slim\nCOPY asset /opt/asset\n")
	put(t, filepath.Join(source.Root, "asset"), "copied")
	put(t, filepath.Join(source.Root, "not-copied.txt"), "ignored")
	put(t, filepath.Join(source.Root, "private/omitted"), "ignored directory content")
	put(t, filepath.Join(source.Root, ".dockerignore"), "not-copied.txt\nprivate\n")
	if _, err := s.Init(ctx, source, InitOptions{Harness: "pi", Artifacts: []string{"Dockerfile"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(get(t, filepath.Join(source.Root, "Dockerfile"))), "COPY asset") {
		t.Fatal("init replaced Dockerfile")
	}
	project, _ := s.Project(t.TempDir())
	if _, err := s.Create(ctx, project, "image"); err != nil {
		t.Fatal(err)
	}
	if string(get(t, filepath.Join(project.Root, "asset"))) != "copied" {
		t.Fatal("build context asset lost")
	}
	if _, err := os.Stat(filepath.Join(project.Root, "not-copied.txt")); !os.IsNotExist(err) {
		t.Fatal("ignored context copied")
	}
	if _, err := os.Stat(filepath.Join(project.Root, "private")); !os.IsNotExist(err) {
		t.Fatal("ignored build directory was mistaken for harness configuration")
	}
	if string(get(t, filepath.Join(project.Root, ".dockerignore"))) != "not-copied.txt\nprivate\n" {
		t.Fatal("context policy lost")
	}
}

func TestSourceTreeAndPublishPreserveExecutableIntent(t *testing.T) {
	s := fixture(t)
	o, _ := s.Profile("scripts")
	put(t, filepath.Join(o.Root, "config.json"), `{"version":1}`)
	put(t, filepath.Join(o.Root, "setup.sh"), "echo hello")
	os.Chmod(filepath.Join(o.Root, "setup.sh"), 0755)
	p, _ := s.Project(t.TempDir())
	if _, err := s.Create(context.Background(), p, "scripts"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(p.Root, "setup.sh"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("executable intent lost", err)
	}
	if err = fsutil.WriteNew(filepath.Join(p.Root, "setup.sh"), []byte("overwrite"), 0600); !os.IsExist(err) {
		t.Fatal("no-replace write replaced a file", err)
	}
	if !strings.Contains(string(get(t, filepath.Join(p.Root, "setup.sh"))), "hello") {
		t.Fatal("existing script changed")
	}
}
