package environment

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func putBuild(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestImagePlansRespectCapturedContext(t *testing.T) {
	h, err := harness.Load(t.TempDir(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	normal := filepath.Join(root, "Dockerfile")
	putBuild(t, normal, "FROM debian:bookworm-slim\nCOPY included /opt/included\n")
	putBuild(t, filepath.Join(root, "included"), "one")
	putBuild(t, filepath.Join(root, "ignored"), "private")
	putBuild(t, filepath.Join(root, ".dockerignore"), "ignored\n")
	first, err := PlanImage(map[string]string{"Dockerfile": normal}, h.Definition, 1000, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode != "normal" || first.Arguments["HOST_GID"] != "1001" || !strings.HasPrefix(string(first.FinalDockerfile("devbox-rewrite/build:base")), "FROM devbox-rewrite/build:base\nUSER root\n") {
		t.Fatal("incorrect normal plan")
	}
	if _, ok := first.Context["ignored"]; ok {
		t.Fatal("ignored file captured")
	}
	hash := first.InputFingerprint()
	putBuild(t, filepath.Join(root, "ignored"), "changed")
	second, err := PlanImage(map[string]string{"Dockerfile": normal}, h.Definition, 1000, 1001)
	if err != nil || second.InputFingerprint() != hash {
		t.Fatal("excluded file caused drift", err)
	}
	putBuild(t, filepath.Join(root, "included"), "two")
	second, err = PlanImage(map[string]string{"Dockerfile": normal}, h.Definition, 1000, 1001)
	if err != nil || second.InputFingerprint() == hash {
		t.Fatal("context change missed", err)
	}
	if string(first.Context["included"].Data) != "one" {
		t.Fatal("captured plan reread source")
	}
	putBuild(t, normal+".dockerignore", "ignored\nincluded\n")
	plan, err := PlanImage(map[string]string{"Dockerfile": normal}, h.Definition, 1000, 1001)
	if err != nil {
		t.Fatal(err)
	}
	text := string(plan.FinalDockerfile("devbox-rewrite/build:base"))
	if !strings.Contains(text, "pi.dev/install") || !strings.Contains(text, "USER devuser") {
		t.Fatal("Devbox must always install its runtime and harness")
	}
	if _, ok := plan.Context["included"]; ok {
		t.Fatal("Dockerfile-specific ignore did not win")
	}
}
func TestBuildContextNegationsAndUnsafeInputs(t *testing.T) {
	h, _ := harness.Load(t.TempDir(), "pi")
	root := t.TempDir()
	file := filepath.Join(root, "Dockerfile")
	putBuild(t, file, "FROM debian:bookworm-slim\n")
	putBuild(t, filepath.Join(root, "folder/keep"), "yes")
	putBuild(t, filepath.Join(root, "folder/drop"), "no")
	putBuild(t, filepath.Join(root, ".dockerignore"), "folder\n!folder/keep\n")
	plan, err := PlanImage(map[string]string{"Dockerfile": file}, h.Definition, 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if string(plan.Context["folder/keep"].Data) != "yes" {
		t.Fatal("negated child missing")
	}
	if _, ok := plan.Context["folder/drop"]; ok {
		t.Fatal("excluded child included")
	}
	if err = os.Symlink("Dockerfile", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err = PlanImage(map[string]string{"Dockerfile": file}, h.Definition, 1000, 1000); err == nil {
		t.Fatal("unsafe context accepted")
	}
}
func TestRuntimeMountParentsArePreparedAsUserInBothBuildModes(t *testing.T) {
	h, err := harness.Load(t.TempDir(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	command := mountParentCommand(h.Definition)
	for _, want := range []string{"/home/devuser/.local", "/home/devuser/.local/share", "/home/devuser/.config", "/home/devuser/.cache"} {
		if !slices.Contains(command, want) {
			t.Fatal("missing image parent", want)
		}
	}
	for _, notImage := range []string{"/home/devuser/.local/share/opencode", "/home/devuser/.local/state"} {
		if slices.Contains(command, notImage) {
			t.Fatal("prepared a mounted or undeclared directory", notImage)
		}
	}
	encoded, _ := json.Marshal(command)
	instruction := "RUN " + string(encoded) + "\n"
	source := filepath.Join(t.TempDir(), "Dockerfile")
	putBuild(t, source, "FROM debian:bookworm-slim\n")
	for _, winners := range []map[string]string{{}, {"Dockerfile": source}} {
		plan, err := PlanImage(winners, h.Definition, 1000, 1000)
		if err != nil {
			t.Fatal(err)
		}
		text := string(plan.FinalDockerfile("devbox-rewrite/build:custom"))
		user := strings.Index(text, "USER devuser\n")
		parents := strings.Index(text, instruction)
		if user < 0 || parents < user || strings.Contains(text[user:], "USER root\n") {
			t.Fatal("parent preparation must run as devuser in the final layer", text)
		}
	}
}
func TestMountParentCommandCreatesWritableAncestorsWithLiteralPaths(t *testing.T) {
	d := harness.Definition{Stores: []harness.Store{{Target: "/home/devuser/.custom/$literal; space/data"}}}
	args := mountParentCommand(d)
	home := t.TempDir()
	for i := 5; i < len(args); i++ {
		args[i] = strings.Replace(args[i], "/home/devuser", home, 1)
	}
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// This sibling is not a mount target. Its creation reproduces the operation
	// that failed for OpenCode when Docker supplied a root-owned ancestor.
	if err := os.Mkdir(filepath.Join(home, ".custom", "state"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".custom", "$literal; space")); err != nil {
		t.Fatal("path was interpreted as shell syntax", err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	putBuild(t, blocked, "keep")
	args = mountParentCommand(harness.Definition{Stores: []harness.Store{{Target: "/home/devuser/sub/data"}}})
	for i := 5; i < len(args); i++ {
		args[i] = strings.Replace(args[i], "/home/devuser", blocked, 1)
	}
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err == nil {
		t.Fatal("incompatible parent was accepted", string(out))
	}
}
func TestMountParentCommandRejectsUnwritableImageParents(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks require a non-root process, like the image's devuser")
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(home, 0700)
	args := mountParentCommand(harness.Definition{Stores: []harness.Store{{Target: "/home/devuser/data"}}})
	args[5] = home
	output, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "not writable/searchable") {
		t.Fatal("incompatible parent permissions were ignored", err, string(output))
	}
	info, err := os.Stat(home)
	if err != nil || info.Mode().Perm() != 0500 {
		t.Fatal("existing parent permissions were changed", err)
	}
}
func TestRuntimeLayerHonorsHostIDs(t *testing.T) {
	h, _ := harness.Load(t.TempDir(), "opencode")
	text := string(ImageDockerfile(h.Definition, 1234, 5678))
	for _, want := range []string{"-u 1234", "-g 5678", "opencode.ai/install", "USER devuser"} {
		if !strings.Contains(text, want) {
			t.Fatalf("template missing %s", want)
		}
	}
}
