package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func generatedCompletion(t *testing.T, shell string, noDescriptions bool) string {
	t.Helper()
	cmd := New()
	if cmd.HasAlias("devbox-neo") {
		t.Fatal("the old executable remains a CLI alias")
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

func TestCompletionScriptsUseDBX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEVBOX_HOME", filepath.Join(home, "uninitialized"))
	t.Setenv("PATH", t.TempDir())
	before := completionSnapshot(t, home)
	for _, tt := range []struct {
		shell string
		want  string
	}{
		{"bash", "complete -o default -F __start_dbx dbx"},
		{"zsh", "compdef _dbx dbx"},
		{"fish", "complete -c dbx -n '__dbx_clear_perform_completion_once_result'"},
		{"powershell", "Register-ArgumentCompleter -CommandName 'dbx'"},
	} {
		for _, noDescriptions := range []bool{false, true} {
			script := generatedCompletion(t, tt.shell, noDescriptions)
			if !strings.Contains(script, tt.want) {
				t.Fatalf("%s script missing %q", tt.shell, tt.want)
			}
			if strings.Contains(script, "devbox-neo") {
				t.Fatalf("%s script registers the old executable", tt.shell)
			}
			if strings.Contains(script, "__completeNoDesc") != noDescriptions {
				t.Fatal("description flag ignored", tt.shell)
			}
		}
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("generating completion initialized state")
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
