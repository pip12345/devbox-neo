package environment

import (
	"os"
	"path/filepath"
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
	if first.Mode != "normal" || first.Arguments["HOST_GID"] != "1001" || !strings.HasPrefix(string(first.FinalDockerfile("sha256:base")), "FROM sha256:base\nUSER root\n") {
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
	text := string(plan.FinalDockerfile("sha256:base"))
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
func TestRuntimeLayerHonorsHostIDs(t *testing.T) {
	h, _ := harness.Load(t.TempDir(), "opencode")
	text := string(ImageDockerfile(h.Definition, 1234, 5678))
	for _, want := range []string{"-u 1234", "-g 5678", "opencode.ai/install", "USER devuser"} {
		if !strings.Contains(text, want) {
			t.Fatalf("template missing %s", want)
		}
	}
}
