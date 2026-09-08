package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTerminalEnvAllowlist(t *testing.T) {
	host := map[string]string{
		"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "",
		"CLICOLOR": "1", "CLICOLOR_FORCE": "1", "FORCE_COLOR": "1",
		"TERM_PROGRAM": "WezTerm", "TERM_PROGRAM_VERSION": "1", "WT_SESSION": "session",
		"WEZTERM_EXECUTABLE": "/bin/wezterm", "KITTY_WINDOW_ID": "2", "VTE_VERSION": "3",
		"KONSOLE_VERSION": "4", "ITERM_SESSION_ID": "5",
		"API_TOKEN": "not-terminal-data", "PS1": "host-prompt", "PATH": "/host/bin",
	}
	got := TerminalEnv(func(key string) (string, bool) { value, ok := host[key]; return value, ok })
	want := []string{
		"TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR=", "CLICOLOR=1",
		"CLICOLOR_FORCE=1", "FORCE_COLOR=1", "TERM_PROGRAM=WezTerm", "TERM_PROGRAM_VERSION=1",
		"WT_SESSION=session", "WEZTERM_EXECUTABLE=/bin/wezterm", "KITTY_WINDOW_ID=2",
		"VTE_VERSION=3", "KONSOLE_VERSION=4", "ITERM_SESSION_ID=5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("terminal env = %q, want %q", got, want)
	}
	if got := TerminalEnv(func(string) (string, bool) { return "", false }); len(got) != 0 {
		t.Fatal("unset terminal values were synthesized", got)
	}
}

func TestTerminalEnvCreationAttachmentAndRecovery(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","extra_env":["TERM=configured"]}`)
	e.TerminalEnv = []string{"TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR="}
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	var createdEnv [][]string
	d.Fail = func(args []string) error {
		if args[0] == "create" {
			idx := slices.Index(args, "--env-file")
			if idx < 0 {
				return errors.New("missing creation env")
			}
			data, err := os.ReadFile(args[idx+1])
			if err != nil {
				return err
			}
			createdEnv = append(createdEnv, strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"))
		}
		return nil
	}
	assertAttached := func(wantArg string) {
		t.Helper()
		var last []string
		for _, args := range d.History() {
			if args[0] == "exec" {
				last = args
			}
		}
		for _, entry := range e.TerminalEnv {
			idx := slices.Index(last, entry)
			if idx < 1 || last[idx-1] != "--env" {
				t.Fatalf("attached command missing %q: %q", entry, last)
			}
		}
		if last[len(last)-1] != wantArg {
			t.Fatal("wrong attached command", last)
		}
	}
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	assertAttached("pi")
	wantCreated := append(slices.Clone(e.TerminalEnv), spec.Env()...)
	if !slices.Equal(createdEnv[0], wantCreated) {
		t.Fatalf("creation must put terminal defaults before configured env: %q", createdEnv[0])
	}
	first := record(t, e, result.Name)
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", result.Name, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"xterm-256color", "COLORTERM", "truecolor", "NO_COLOR"} {
		if strings.Contains(string(data), value) {
			t.Fatalf("terminal data %q persisted in session", value)
		}
	}
	e.TerminalEnv = []string{"TERM=screen-256color", "COLORTERM=24bit"}
	reopened, err := e.Open(ctx, q)
	if err != nil || len(reopened.Diagnostics) != 0 || count(d, "create") != 1 {
		t.Fatal("terminal change caused drift or recreation", reopened, err)
	}
	assertAttached("pi")
	if err := e.Exec(ctx, result.Name, "", nil, true); err != nil {
		t.Fatal(err)
	}
	assertAttached("bash")
	if err := e.Exec(ctx, result.Name, "", []string{"printenv", "TERM"}, false); err != nil {
		t.Fatal(err)
	}
	assertAttached("TERM")
	c, _ := d.Snapshot(result.Name)
	if err := e.Docker.Remove(ctx, c, e.owner(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, result.Name, ""); err != nil {
		t.Fatal(err)
	}
	if len(createdEnv) != 2 || !slices.Equal(createdEnv[1], append(slices.Clone(e.TerminalEnv), spec.Env()...)) {
		t.Fatal("recovery did not use current terminal defaults", createdEnv)
	}
	if recovered := record(t, e, result.Name); recovered.ID != first.ID || recovered.Applied != first.Applied {
		t.Fatal("terminal changed the recorded identity or fingerprints")
	}
}
