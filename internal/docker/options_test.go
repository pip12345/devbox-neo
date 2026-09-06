package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMountNormalizationAndOwnedTargetProtection(t *testing.T) {
	workspace := t.TempDir()
	file := filepath.Join(workspace, "file")
	os.WriteFile(file, []byte("x"), 0600)
	mount, err := ParseMount("./file:/data/file:ro", workspace, t.TempDir())
	if err != nil || mount.Source != file || !mount.File || !mount.ReadOnly {
		t.Fatal(mount, err)
	}
	volume, err := ParseMount("shared:/data:rw", workspace, "")
	if err != nil || volume.Kind != "volume" {
		t.Fatal(volume, err)
	}
	for _, target := range []string{"/", "/workspace", "/workspace/sub", "/home", "/home/devuser/.pi/agent/sub"} {
		if err = ValidateExtraTargets([]Mount{{Target: target}}, []string{"/workspace", "/home/devuser/.pi/agent"}); err == nil {
			t.Fatal("managed target shadowed", target)
		}
	}
	if _, err = ParseMount("./missing:/data", workspace, ""); err == nil {
		t.Fatal("missing bind source accepted")
	}
}
func TestPortValidation(t *testing.T) {
	for _, value := range []string{"80", "8080:80", "127.0.0.1:8080:80", "[::1]:8080:80", "8000-8002:9000-9002/udp", ":80"} {
		if err := ValidatePort(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"0", "65536", "bad", "host:80:80", "1-3:1-2", "80/http", "1-0", "::1:80:80"} {
		if err := ValidatePort(value); err == nil {
			t.Fatal("invalid port accepted", value)
		}
	}
}
func TestRawArgumentsProtectManagedBoundaries(t *testing.T) {
	protected := []string{"/workspace", "/devbox", "/home/devuser/.pi/agent"}
	for _, value := range []string{"--name=other", "--entrypoint=bash", "--user=root", "--network=host", "--env=DEVBOX_HOST=wrong", "--label=devcontainer.metadata={}", "--label=" + Namespace + ".session=fake", "--mount=type=tmpfs,dst=/workspace/sub", "--tmpfs=/devbox", "--add-host=host.docker.internal:1.2.3.4", "image", "--", "--memory"} {
		if err := ValidateRaw([]string{value}, protected, t.TempDir(), t.TempDir()); err == nil {
			t.Fatal("unsafe raw argument accepted", value)
		}
	}
	if err := ValidateRaw([]string{"--memory=512m", "--cap-add=SYS_PTRACE", "--privileged", "--label=team=dev", "--env=PUBLIC=value"}, protected, t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
