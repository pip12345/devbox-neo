package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/config"
)

func configDirectoryFixture(t *testing.T) (Service, Owner) {
	t.Helper()
	s := Service{Home: t.TempDir()}
	o, err := s.ConfigDirectory("base", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if o.Root != filepath.Join(s.Home, "configs/base") {
		t.Fatal(o)
	}
	return s, o
}

func TestCreateConfigRejectsRepeatAndPreservesExistingFiles(t *testing.T) {
	ctx := context.Background()
	s, o := configDirectoryFixture(t)
	if err := os.MkdirAll(o.Root, 0700); err != nil {
		t.Fatal(err)
	}
	setup := filepath.Join(o.Root, "setup.sh")
	if err := os.WriteFile(setup, []byte("user script"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := s.CreateConfig(ctx, o, SetupOptions{Artifacts: []string{"setup.sh"}})
	if err != nil || len(result.Created) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("create: %+v %v", result, err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	layer, err := config.ParseLayer(data)
	if err != nil || layer.Harness != nil {
		t.Fatalf("minimal config: %+v %v", layer, err)
	}
	_, err = s.CreateConfig(ctx, o, SetupOptions{Artifacts: []string{"before-open.sh"}})
	var actionable *commanderror.Error
	if !errors.As(err, &actionable) || actionable.Code != "config_exists" {
		t.Fatalf("repeat create: %v", err)
	}
	after, _ := os.ReadFile(result.Path)
	if string(after) != string(data) {
		t.Fatal("repeat creation changed config")
	}
	after, _ = os.ReadFile(setup)
	if string(after) != "user script" {
		t.Fatal("creation overwrote existing artifact")
	}
	if _, err := os.Stat(filepath.Join(o.Root, "before-open.sh")); !os.IsNotExist(err) {
		t.Fatalf("repeat creation added files: %v", err)
	}
	for _, content := range []string{"", "invalid"} {
		if err := os.WriteFile(result.Path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckConfigCreation(o); !errors.As(err, &actionable) || actionable.Code != "config_exists" {
			t.Fatalf("existing invalid config was not rejected: %v", err)
		}
	}
}

func TestConfigArtifactsDoNotSelectOrChangeHarness(t *testing.T) {
	ctx := context.Background()
	s, o := configDirectoryFixture(t)
	result, err := s.CreateConfig(ctx, o, SetupOptions{Artifacts: []string{"harness-config"}, ArtifactHarness: "pi"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(result.Path)
	layer, err := config.ParseLayer(data)
	if err != nil || layer.Harness != nil {
		t.Fatalf("artifact generation selected a harness: %+v %v", layer, err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, "pi")); err != nil {
		t.Fatal("missing generated Pi tree", err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, "pi/skills/devbox/SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("copied inherited Devbox skill", err)
	}
	if _, err := s.EditConfig(ctx, o, SetupOptions{Artifacts: []string{"harness-config"}, ArtifactHarness: "opencode"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(result.Path)
	if string(after) != string(data) {
		t.Fatal("adding a second harness tree changed config settings")
	}
	pi := "pi"
	if _, err := s.EditConfig(ctx, o, SetupOptions{Harness: &pi, Artifacts: []string{"harness-config"}, ArtifactHarness: "opencode"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(result.Path)
	layer, _ = config.ParseLayer(data)
	if layer.Harness == nil || *layer.Harness != "pi" {
		t.Fatal("file-generation target overrode explicit harness setting")
	}
	for _, name := range []string{"pi", "opencode"} {
		if _, err := os.Stat(filepath.Join(o.Root, name)); err != nil {
			t.Fatal("missing generated tree", name, err)
		}
	}
}

func TestConfigSetupPreflightsBeforeMutation(t *testing.T) {
	for _, options := range []SetupOptions{
		{Artifacts: []string{"harness-config"}},
		{ArtifactHarness: "pi"},
		{Artifacts: []string{"not-an-artifact"}},
		{Artifacts: []string{"harness-config"}, ArtifactHarness: "not-a-harness"},
	} {
		s, o := configDirectoryFixture(t)
		if _, err := s.CreateConfig(context.Background(), o, options); err == nil {
			t.Fatalf("accepted %+v", options)
		}
		if _, err := os.Stat(o.Root); !os.IsNotExist(err) {
			t.Fatalf("invalid setup created its config directory: %v", err)
		}
	}
	s, o := configDirectoryFixture(t)
	if _, err := s.EditConfig(context.Background(), o, SetupOptions{Artifacts: []string{"setup.sh"}}); err == nil {
		t.Fatal("edit created a missing config")
	}
	if _, err := s.CreateConfig(context.Background(), o, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(o.Root, "config.json"))
	if err := os.Mkdir(filepath.Join(o.Root, "setup.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	pi := "pi"
	if _, err := s.EditConfig(context.Background(), o, SetupOptions{Harness: &pi, Artifacts: []string{"setup.sh"}}); err == nil {
		t.Fatal("accepted directory as artifact destination")
	}
	after, _ := os.ReadFile(filepath.Join(o.Root, "config.json"))
	if string(before) != string(after) {
		t.Fatal("artifact preflight failure published settings")
	}
}

func TestConcurrentConfigCreationPublishesOnlyWinnerArtifacts(t *testing.T) {
	ctx := context.Background()
	s, o := configDirectoryFixture(t)
	type outcome struct {
		artifact string
		err      error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"setup.sh", "before-open.sh"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateConfig(ctx, o, SetupOptions{Artifacts: []string{name}})
			outcomes <- outcome{name, err}
		}()
	}
	wg.Wait()
	close(outcomes)
	winners := 0
	for result := range outcomes {
		_, existsErr := os.Stat(filepath.Join(o.Root, result.artifact))
		if result.err == nil {
			winners++
			if existsErr != nil {
				t.Fatal("winning creator did not add its files", existsErr)
			}
		} else {
			var actionable *commanderror.Error
			if !errors.As(result.err, &actionable) || actionable.Code != "config_exists" || !os.IsNotExist(existsErr) {
				t.Fatalf("losing creator: %v, artifact: %v", result.err, existsErr)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful creators", winners)
	}
}

func TestArtifactTargetDoesNotResolveUnrelatedHarnessExpression(t *testing.T) {
	s, o := configDirectoryFixture(t)
	if _, err := s.CreateConfig(context.Background(), o, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.Root, "config.json")
	original := `{"version":1,"harness":"${env:DEVBOX_TEST_UNSET_HARNESS}"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.EditConfig(context.Background(), o, SetupOptions{Artifacts: []string{"harness-config"}, ArtifactHarness: "pi"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != original {
		t.Fatal("artifact setup rewrote a source expression")
	}
}
