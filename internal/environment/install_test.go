package environment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func TestHarnessInstallImageUsesCapturedFiles(t *testing.T) {
	home := t.TempDir()
	base, err := harness.Load(t.TempDir(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	base.Definition.Install.Shell, base.Definition.Install.Script = "", "install.sh"
	definition, err := json.Marshal(base.Definition)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "harnesses", "pi")
	putBuild(t, filepath.Join(root, "harness.json"), string(definition))
	putBuild(t, filepath.Join(root, "install", "install.sh"), "true\n")
	putBuild(t, filepath.Join(root, "install", "helper.sh"), "old helper\n")
	h, err := harness.Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	putBuild(t, filepath.Join(root, "install", "helper.sh"), "new helper\n")
	plan, err := PlanImage(nil, "debian:bookworm-slim", h, 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if string(plan.InstallContext["harness-install/helper.sh"].Data) != "old helper\n" {
		t.Fatal("image compilation reread the installation source")
	}
	text := string(plan.Runtime)
	copy := strings.Index(text, "COPY --chown=devuser:devuser")
	entry := strings.Index(text, `"/tmp/devbox-harness-install/install.sh"`)
	clean := strings.Index(text, `RUN ["rm", "-rf", "/tmp/devbox-harness-install"]`)
	user := strings.Index(text, "USER devuser\n")
	if user < 0 || copy < user || entry < copy || clean < entry {
		t.Fatal("installation files were not copied, run as devuser, and cleaned in order", text)
	}
	old := plan.inputs(h, "test").fingerprint()
	changed, err := harness.Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	next, err := PlanImage(nil, "debian:bookworm-slim", changed, 1000, 1000)
	if err != nil || next.inputs(changed, "test").fingerprint() == old {
		t.Fatal("helper edits did not require a new image", err)
	}
	if err := os.Chmod(filepath.Join(root, "install", "helper.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	mode, err := harness.Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	modePlan, err := PlanImage(nil, "debian:bookworm-slim", mode, 1000, 1000)
	if err != nil || modePlan.inputs(mode, "test").fingerprint() == next.inputs(changed, "test").fingerprint() {
		t.Fatal("helper modes did not require a new image", err)
	}
	h.InstallFiles = nil
	if _, err := PlanImage(nil, "debian:bookworm-slim", h, 1000, 1000); err == nil {
		t.Fatal("scripted installation accepted uncaptured inputs")
	}
}
