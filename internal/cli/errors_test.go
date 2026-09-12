package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"github.com/spf13/cobra"
)

func TestResourceJSONErrorsAreStructuredOnce(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	cmd := New()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--home", home, "profile", "init", "missing", "--harness", "pi", "--json"})
	if code := Execute(context.Background(), cmd); code != 1 {
		t.Fatal(code)
	}
	var report errorReport
	decoder := json.NewDecoder(&out)
	if err := decoder.Decode(&report); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("multiple output payloads", err)
	}
	want := []string{"devbox-neo", "--home", home, "profile", "create", "missing"}
	if report.Code != "owner_missing" || report.Operation != "devbox-neo profile init" || len(report.Next) != 1 || !reflect.DeepEqual(report.Next[0].Command, want) || report.Target != filepath.Join(home, "profiles/missing/config.json") {
		t.Fatal(report)
	}
	if stderr.Len() != 0 {
		t.Fatal("JSON error also printed as text", stderr.String())
	}
}

func TestErrorRenderingPreservesStreamsStatusAndJoinedCleanup(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		root := &cobra.Command{Use: "devbox-neo", SilenceErrors: true, SilenceUsage: true}
		var jsonFlag bool
		foreground := &docker.ExitError{Code: 23, Operation: "exec"}
		failure := errors.Join(commanderror.New("docker_command_failed", "foreground failed", "container", foreground),
			commanderror.New("cleanup_failed", "session state was retained; cleanup failed", "container", nil))
		cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, _ []string) error {
			if !jsonFlag {
				fmt.Fprintln(cmd.OutOrStdout(), "child output")
			}
			return failure
		}}
		cmd.Flags().BoolVar(&jsonFlag, "json", false, "")
		root.AddCommand(cmd)
		var out, stderr bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&stderr)
		args := []string{"test"}
		if asJSON {
			args = append(args, "--json")
		}
		root.SetArgs(args)
		if code := Execute(context.Background(), root); code != 23 {
			t.Fatal("foreground status lost", code)
		}
		if asJSON {
			var report errorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Code != "docker_command_failed" || len(report.Related) != 1 || report.Related[0].Code != "cleanup_failed" || stderr.Len() != 0 {
				t.Fatal(report, stderr.String())
			}
		} else if out.String() != "child output\n" || strings.Count(stderr.String(), "Error:") != 2 || !strings.Contains(stderr.String(), "cleanup failed") {
			t.Fatal("stream output or cleanup diagnostic lost", out.String(), stderr.String())
		}
	}
}

func TestCommandErrorCausesRemainPrivateAndDetectable(t *testing.T) {
	cause := errors.New("credential-value-do-not-print")
	actionable := commanderror.New("invalid_configuration", "Invalid environment assignment", "/public/config.json", cause)
	wrapped := fmt.Errorf("preflight: %w", actionable)
	if !errors.Is(wrapped, cause) {
		t.Fatal("cause identity lost")
	}
	var typed *commanderror.Error
	if !errors.As(wrapped, &typed) {
		t.Fatal("typed error lost")
	}
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", true, "")
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	if code := RenderError(cmd, wrapped); code != 1 {
		t.Fatal(code)
	}
	if strings.Contains(out.String()+stderr.String(), cause.Error()) || !strings.Contains(out.String(), "preflight: Invalid environment assignment") {
		t.Fatal(out.String(), stderr.String())
	}
}

func TestInvalidFlagsDoNotExposeRejectedValuesOrInitializeHome(t *testing.T) {
	home := t.TempDir()
	before := completionSnapshot(t, home)
	cmd := New()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--home", home, "status", "--json", "--unknown=private-value"})
	if code := Execute(context.Background(), cmd); code != 1 {
		t.Fatal(code)
	}
	var report errorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, out.String(), stderr.String())
	}
	if report.Code != "invalid_flags" || len(report.Next) != 1 || strings.Contains(out.String()+stderr.String(), "private-value") {
		t.Fatal(report)
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("argument failure initialized home")
	}
}

func TestMissingDockerProducesStructuredFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cmd := New()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--home", t.TempDir(), "list", "--json"})
	if code := Execute(context.Background(), cmd); code != 1 {
		t.Fatal(code)
	}
	var report errorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Code != "docker_unavailable" || stderr.Len() != 0 {
		t.Fatal(report, stderr.String())
	}
}

func TestConfigurationAndHarnessFailuresUseSharedCodes(t *testing.T) {
	for _, tt := range []struct{ config, definition, code string }{
		{`{"version":1,"harness":"pi","extra_env":["private-value"]}`, "", "invalid_configuration"},
		{`{"version":1,"harness":"not-installed"}`, "", "unknown_harness"},
		{`{"version":1,"harness":"pi"}`, `{"version":1,"name":"pi","env":{"TOKEN":"private-value"},"config":"bad"}`, "invalid_harness_definition"},
	} {
		home := t.TempDir()
		completionFile(t, home, "profiles/basic/config.json", tt.config)
		if tt.definition != "" {
			completionFile(t, home, "harnesses/pi/harness.json", tt.definition)
		}
		cmd := New()
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"--home", home, "profile", "config", "basic", "--show", "--json"})
		if code := Execute(context.Background(), cmd); code != 1 {
			t.Fatal(code)
		}
		var report errorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err, out.String())
		}
		if report.Code != tt.code || stderr.Len() != 0 || strings.Contains(out.String(), "private-value") {
			t.Fatal(report, stderr.String())
		}
	}
}

func TestErrorNextStepsScopeOnlyDevboxCommands(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("home", "", "")
	home := "/work/a'b $(no-command)"
	if err := cmd.Flags().Set("home", home); err != nil {
		t.Fatal(err)
	}
	next := []commanderror.Step{commanderror.Next("Inspect", "session", "show", "container"), {Command: []string{"docker", "info"}}}
	scoped := scopedSteps(cmd, next, home)
	if !reflect.DeepEqual(scoped[1].Command, next[1].Command) || !reflect.DeepEqual(next[0].Command, []string{"devbox-neo", "session", "show", "container"}) {
		t.Fatal("external command or source steps mutated")
	}
	if text := stepsText(scoped); !strings.Contains(text, shellQuote(home)) {
		t.Fatal(text)
	}
}

func TestHumanErrorsUseShortHeaderAndLabeledActions(t *testing.T) {
	cmd := &cobra.Command{Use: "open"}
	cmd.Flags().String("home", "", "")
	if err := cmd.Flags().Set("home", "/home/custom home"); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	failure := commanderror.New("session_exists", "Environment already exists.", "/work/project", errors.New("private cause"),
		commanderror.Next("Open", "open", "/work/project"),
		commanderror.Next("Or recreate with current configuration", "recreate", "/work/project"))
	if code := RenderError(cmd, failure); code != 1 {
		t.Fatal(code)
	}
	want := "Error: Environment already exists.\n" +
		"Target: /work/project\n\n" +
		"Open:\n  devbox-neo --home '/home/custom home' open /work/project\n\n" +
		"Or recreate with current configuration:\n  devbox-neo --home '/home/custom home' recreate /work/project\n"
	if stderr.String() != want || out.Len() != 0 {
		t.Fatalf("got %q; want %q", stderr.String(), want)
	}
}

func TestHumanErrorsKeepDetailsWithoutExposingCauses(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"cancelled", context.Canceled, "Error: Cancelled.\n"},
		{"timeout", context.DeadlineExceeded, "Error: Operation timed out.\n"},
		{"path", &os.PathError{Op: "open", Path: "/work/missing", Err: os.ErrNotExist}, "Error: Cannot access path: file does not exist\nTarget: /work/missing\n"},
		{"validation", commanderror.New("invalid_configuration", "Invalid configuration: network cannot be empty", "/work/config.json", errors.New("private config value")), "Error: Invalid configuration: network cannot be empty\nTarget: /work/config.json\n"},
		{"ownership", commanderror.New("ownership_mismatch", "Cannot verify Devbox ownership of this image: incomplete identity.", "sha256:image", nil), "Error: Cannot verify Devbox ownership of this image: incomplete identity.\nTarget: sha256:image\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			var out bytes.Buffer
			cmd.SetErr(&out)
			if code := RenderError(cmd, tt.err); code != 1 || out.String() != tt.want {
				t.Fatalf("code %d, got %q; want %q", code, out.String(), tt.want)
			}
		})
	}
}

func TestActionLabelsEscapeTerminalControls(t *testing.T) {
	steps := []commanderror.Step{commanderror.Next("Inspect\n\x1b[31m", "session", "show", "a'b")}
	text := stepsText(steps)
	if strings.Contains(text, "\x1b") || !strings.Contains(text, shellQuote("a'b")) || strings.Count(text, "\n") != 2 {
		t.Fatalf("unsafe label or unquoted command: %q", text)
	}
}

func TestCancellationAndMissingPathAreNotSuccess(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
	}{{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"}, {&os.PathError{Op: "open", Path: "/missing", Err: os.ErrNotExist}, "path_unavailable"}} {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().Bool("json", true, "")
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		if code := RenderError(cmd, tt.err); code != 1 {
			t.Fatal(code)
		}
		var report errorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Code != tt.code {
			t.Fatal(report)
		}
	}
}
