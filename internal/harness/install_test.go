package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scriptHarness(t *testing.T, home string) string {
	t.Helper()
	h, err := Load(t.TempDir(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	h.Definition.Install.Shell = ""
	h.Definition.Install.Script = "install.sh"
	b, err := json.Marshal(h.Definition)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "harnesses", "pi")
	if err := os.MkdirAll(filepath.Join(root, "install", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(root, "harness.json"), string(b))
	authWrite(t, filepath.Join(root, "install", "install.sh"), "#!/bin/bash\ntrue\n")
	authWrite(t, filepath.Join(root, "install", "nested", "helper.sh"), "one\n")
	return root
}

func TestInstallationFilesCaptureAndRecordedDigest(t *testing.T) {
	home := t.TempDir()
	root := scriptHarness(t, home)
	first, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.InstallFiles) != 2 || len(first.Defaults) != 0 {
		t.Fatal("install inputs inherited or leaked into configuration defaults")
	}
	_, recorded, err := Recorded("pi", first.Origin)
	if err != nil || recorded != first.Hash {
		t.Fatal("recorded and desired installation contracts disagree", err)
	}
	helper := filepath.Join(root, "install", "nested", "helper.sh")
	authWrite(t, helper, "two\n")
	changed, err := Load(home, "pi")
	if err != nil || changed.Hash == first.Hash {
		t.Fatal("changed helper did not change the definition contract", err)
	}
	if string(first.InstallFiles["nested/helper.sh"].Data) != "one\n" {
		t.Fatal("captured installation reread source")
	}
	_, recorded, err = Recorded("pi", first.Origin)
	if err != nil || recorded != changed.Hash {
		t.Fatal("recorded source ignored changed installation files", err)
	}
	if err := os.Chmod(helper, 0700); err != nil {
		t.Fatal(err)
	}
	mode, err := Load(home, "pi")
	if err != nil || mode.Hash == changed.Hash {
		t.Fatal("installation file modes were not fingerprinted", err)
	}
	if err := os.Mkdir(filepath.Join(root, "defaults"), 0700); err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(root, "defaults", "settings.json"), "{}")
	defaults, err := Load(home, "pi")
	if err != nil || defaults.Hash != mode.Hash {
		t.Fatal("runtime defaults changed the installation digest", err)
	}
}

func TestInstallScriptConstraintsAndMissingInputs(t *testing.T) {
	for _, script := range []string{".", "../outside.sh", "/outside.sh", "nested/../install.sh"} {
		h, err := Load(t.TempDir(), "pi")
		if err != nil {
			t.Fatal(err)
		}
		h.Definition.Install.Shell, h.Definition.Install.Script = "", script
		if err := h.Definition.Validate(); err == nil {
			t.Fatal("unsafe install script accepted", script)
		}
	}
	h, _ := Load(t.TempDir(), "pi")
	h.Definition.Install.Script = "install.sh"
	if err := h.Definition.Validate(); err == nil {
		t.Fatal("install.shell and install.script were both accepted")
	}
	home := t.TempDir()
	root := scriptHarness(t, home)
	if err := os.Remove(filepath.Join(root, "install", "install.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home, "pi"); err == nil {
		t.Fatal("missing selected installer fell back to the built-in")
	}
	if _, _, err := Recorded("pi", filepath.Join(root, "harness.json")); err == nil {
		t.Fatal("recorded source accepted a missing installer")
	}
	outside := filepath.Join(t.TempDir(), "installer.sh")
	authWrite(t, outside, "true")
	if err := os.Symlink(outside, filepath.Join(root, "install", "install.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home, "pi"); err == nil {
		t.Fatal("selected installer followed a symlink")
	}
}

func TestInstallAssetsUseRegularFileReader(t *testing.T) {
	home := t.TempDir()
	root := scriptHarness(t, home)
	if err := os.Symlink("missing", filepath.Join(root, "install", "link")); err != nil {
		t.Fatal(err)
	}
	h, err := Load(home, "pi")
	if err != nil || len(h.InstallFiles) != 2 || len(h.Warnings) != 1 || !strings.Contains(h.Warnings[0], filepath.Join(root, "install", "link")) {
		t.Fatal("installation files bypassed shared source-file rules", err, h.Warnings)
	}
	builtin, err := Load(t.TempDir(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	_, recorded, err := Recorded("opencode", "builtin")
	if err != nil || recorded != builtin.Hash {
		t.Fatal("builtin installation digest was not reproducible", err)
	}
}
