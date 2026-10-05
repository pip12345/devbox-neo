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
cat >"$HOME/.opencode/bin/opencode" <<'NATIVE'
#!/bin/bash
[[ $# == 1 && $1 == --version ]] || exit 64
printf 'opencode v2.0.23\n'
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
			bin := filepath.Join(home, ".opencode", "bin")
			if mode != "success" {
				if err == nil {
					t.Fatal("installer failure was ignored", string(output))
				}
				for _, name := range []string{"opencode", "opencode-native", "opencode-auth.sh"} {
					if _, err := os.Stat(filepath.Join(bin, name)); !os.IsNotExist(err) {
						t.Fatal("failed installation published a launcher or native binary", name, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("installation failed: %v\n%s", err, output)
			}
			for _, name := range []string{"opencode", "opencode-native", "opencode-auth.sh"} {
				path := filepath.Join(bin, name)
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatal("installed file is missing or not executable", name, err)
				}
			}
			wrapper, err := os.ReadFile(filepath.Join(bin, "opencode-auth.sh"))
			if err != nil || string(wrapper) != string(h.InstallFiles["opencode-auth.sh"].Data) {
				t.Fatal("installation changed the captured auth wrapper", err)
			}
			for _, name := range []string{"opencode", "opencode-native"} {
				output, err := exec.Command(filepath.Join(bin, name), "--version").CombinedOutput()
				if err != nil || strings.TrimSpace(string(output)) != "opencode v2.0.23" {
					t.Fatal("native executable was lost or wrapper did not delegate", name, err, string(output))
				}
			}
		})
	}
}
