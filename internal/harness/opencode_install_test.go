package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeStableInstallerPreservesNativeAndWrapper(t *testing.T) {
	h, err := Load(t.TempDir(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "download-failure", "installer-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home, tools := filepath.Join(root, "home"), filepath.Join(root, "tools")
			for _, dir := range []string{home, tools} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for name, file := range h.InstallFiles {
				authWrite(t, filepath.Join(root, name), string(file.Data))
			}
			stubs := map[string]string{
				"sudo": `#!/bin/bash
set -eu
if [[ ${1-} == -E ]]; then shift; exec "$@"; fi
`,
				"curl": `#!/bin/bash
set -eu
[[ $1 == -fsSL && $# == 2 ]] || exit 64
case $2 in
    https://deb.nodesource.com/setup_22.x) printf 'exit 0\n' ;;
    https://opencode.ai/v2/install)
        [[ $INSTALL_MODE != download-failure ]] || exit 22
        cat <<'INSTALLER'
set -eu
[[ $# == 1 && $1 == --no-modify-path ]] || exit 64
[[ $INSTALL_MODE != installer-failure ]] || exit 65
mkdir -p "$HOME/.opencode/bin"
printf '#!/bin/bash\nversion=%q\n' "${INSTALL_VERSION:-2.0.23}" >"$HOME/.opencode/bin/opencode"
cat >>"$HOME/.opencode/bin/opencode" <<'NATIVE'
set -euo pipefail
case ${1-} in
    upgrade|update) curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path ;;
    --version) printf 'opencode v%s\n' "$version" ;;
    *) exit 64 ;;
esac
NATIVE
chmod 0755 "$HOME/.opencode/bin/opencode"
INSTALLER
        ;;
    *) exit 64 ;;
esac
`,
			}
			for name, body := range stubs {
				path := filepath.Join(tools, name)
				authWrite(t, path, body)
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", filepath.Join(root, "install.sh"))
			cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+tools+":"+os.Getenv("PATH"), "INSTALL_MODE="+mode)
			output, err := cmd.CombinedOutput()
			bin := filepath.Join(home, ".opencode", "devbox", "bin")
			native := filepath.Join(home, ".opencode", "bin", "opencode")
			paths := []string{filepath.Join(bin, "opencode"), filepath.Join(bin, "opencode-auth.sh"), native}
			if mode != "success" {
				if err == nil {
					t.Fatal("installer failure was ignored", string(output))
				}
				for _, path := range paths {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("failed installation published a launcher or native binary", path, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("installation failed: %v\n%s", err, output)
			}
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatal("installed file is missing or not executable", path, err)
				}
			}
			wrapper, err := os.ReadFile(filepath.Join(bin, "opencode-auth.sh"))
			if err != nil || string(wrapper) != string(h.InstallFiles["opencode-auth.sh"].Data) {
				t.Fatal("installation changed the captured auth wrapper", err)
			}
			launcher := filepath.Join(bin, "opencode")
			for _, path := range []string{launcher, native} {
				output, err := exec.Command(path, "--version").CombinedOutput()
				if err != nil || strings.TrimSpace(string(output)) != "opencode v2.0.23" {
					t.Fatal("native executable was lost or wrapper did not delegate", path, err, string(output))
				}
			}
			launcherBytes, err := os.ReadFile(launcher)
			if err != nil {
				t.Fatal(err)
			}
			for _, update := range []struct{ method, version string }{
				{"upgrade", "2.0.24"}, {"update", "2.0.25"}, {"installer", "2.0.26"},
			} {
				t.Run(update.method, func(t *testing.T) {
					cmd := exec.Command("bash", "-c", `opencode "$1"`, "test-update", update.method)
					if update.method == "installer" {
						cmd = exec.Command("bash", "-c", "set -o pipefail; curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path")
					}
					cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+filepath.Dir(native)+":"+tools+":"+os.Getenv("PATH"), "INSTALL_MODE=success", "INSTALL_VERSION="+update.version)
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("update failed: %v: %s", err, output)
					}
					for _, path := range []string{launcher, native} {
						output, err := exec.Command(path, "--version").CombinedOutput()
						if err != nil || strings.TrimSpace(string(output)) != "opencode v"+update.version {
							t.Fatal("launcher did not select the updated native binary", path, err, string(output))
						}
					}
					if got, err := os.ReadFile(launcher); err != nil || string(got) != string(launcherBytes) {
						t.Fatal("native update replaced the managed launcher", err)
					}
					if got, err := os.ReadFile(filepath.Join(bin, "opencode-auth.sh")); err != nil || string(got) != string(wrapper) {
						t.Fatal("native update changed shared-auth synchronization", err)
					}
				})
			}
		})
	}
}
