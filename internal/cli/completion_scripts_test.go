package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func generatedCompletion(t *testing.T, shell string, noDescriptions bool) string {
	t.Helper()
	cmd := New()
	if cmd.HasAlias("dbx") {
		t.Fatal("completion registration added a CLI command alias")
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	args := []string{"completion", shell}
	if noDescriptions {
		args = append(args, "--no-descriptions")
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestCompletionScriptsRegisterExistingDBXShortcut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEVBOX_HOME", filepath.Join(home, "uninitialized"))
	t.Setenv("PATH", t.TempDir())
	before := completionSnapshot(t, home)
	for _, tt := range []struct {
		shell                        string
		canonical, shortcut, handler string
	}{
		{"bash", "complete -o default -F __start_devbox-neo devbox-neo", "complete -o default -F __start_devbox-neo dbx", "__start_devbox-neo()"},
		{"zsh", "compdef _devbox-neo devbox-neo", "compdef _devbox-neo dbx", "_devbox-neo()"},
		{"fish", "complete -c devbox-neo -n '__devbox_neo_clear_perform_completion_once_result'", "complete -c dbx -n '__devbox_neo_clear_perform_completion_once_result'", "function __devbox_neo_prepare_completions"},
		{"powershell", "Register-ArgumentCompleter -CommandName 'devbox-neo'", "Register-ArgumentCompleter -CommandName 'dbx' -ScriptBlock ${__devbox_neoCompleterBlock}", "[scriptblock]${__devbox_neoCompleterBlock}"},
	} {
		for _, noDescriptions := range []bool{false, true} {
			script := generatedCompletion(t, tt.shell, noDescriptions)
			for _, want := range []string{tt.canonical, tt.shortcut, tt.handler} {
				if !strings.Contains(script, want) {
					t.Fatalf("%s script missing %q", tt.shell, want)
				}
			}
			if strings.Contains(script, "__completeNoDesc") != noDescriptions {
				t.Fatal("description flag ignored", tt.shell)
			}
			if tt.shell == "zsh" && !strings.HasPrefix(script, "#compdef devbox-neo dbx\n") {
				t.Fatal("zsh autoload cannot discover dbx")
			}
			if tt.shell == "fish" {
				for _, want := range []string{"complete -c dbx -n 'not __devbox_neo_requires_order_preservation && __devbox_neo_prepare_completions'", "complete -k -c dbx -n '__devbox_neo_requires_order_preservation && __devbox_neo_prepare_completions'"} {
					if !strings.Contains(script, want) {
						t.Fatal("missing fish directive handling", want)
					}
				}
			}
			for _, forbidden := range []string{"alias dbx=", "alias dbx ", "function dbx", "Set-Alias", "New-Alias", "complete -c dbx -w"} {
				if strings.Contains(script, forbidden) {
					t.Fatal("script defines or redirects the shortcut", tt.shell, forbidden)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("generating completion initialized state")
	}
}

func TestBashDBXCompletionPreservesShortcutAndFlags(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("Bash is required for this Linux completion test", err)
	}
	root := t.TempDir()
	script := filepath.Join(root, "completion.bash")
	if err := os.WriteFile(script, []byte(generatedCompletion(t, "bash", false)), 0600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(root, "devbox stub")
	if err := os.WriteFile(stub, []byte("#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE_FILE\"\nprintf 'basic\\n:4\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "selected home")
	capture := filepath.Join(root, "arguments")
	for _, kind := range []string{"alias", "function"} {
		cmd := exec.Command(bash, "--noprofile", "--norc", "-c", `
set -eu
shopt -s expand_aliases
stub=$2
selected_home=$3
if [[ $4 == alias ]]; then
    printf -v shortcut '%q --home %q' "$stub" "$selected_home"
    alias dbx="$shortcut"
    original=$(alias dbx)
else
    dbx() { "$stub" --home "$selected_home" "$@"; }
    original=$(declare -f dbx)
fi
source "$1"
if [[ $4 == alias ]]; then
    [[ $(alias dbx) == "$original" ]]
else
    [[ $(declare -f dbx) == "$original" ]]
fi
[[ $(complete -p dbx) == *'-F __start_devbox-neo dbx' ]]
[[ $(complete -p devbox-neo) == *'-F __start_devbox-neo devbox-neo' ]]
words=(dbx edit b)
cur=b
__devbox-neo_get_completion_results
[[ $out == $'basic\n' && $directive == 4 ]]
`, "bash", script, stub, home, kind)
		cmd.Env = append(os.Environ(), "CAPTURE_FILE="+capture, "BASH_COMP_DEBUG_FILE=")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(kind, err, string(out))
		}
		b, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--home", home, "__complete", "edit", "b"}
		if got := strings.Split(strings.TrimSpace(string(b)), "\n"); !reflect.DeepEqual(got, want) {
			t.Fatal("completion bypassed shortcut flags", kind, got)
		}
	}
}

type brokenCompletionWriter struct{ err error }

func (w brokenCompletionWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCompletionScriptWriteErrorsPropagate(t *testing.T) {
	failure := errors.New("output unavailable")
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		cmd := New()
		cmd.SetArgs([]string{"completion", shell})
		cmd.SetOut(brokenCompletionWriter{failure})
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); !errors.Is(err, failure) {
			t.Fatal(shell, err)
		}
	}
}
