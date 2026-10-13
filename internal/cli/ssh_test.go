package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/sshshare"
	"github.com/spf13/cobra"
)

func TestSSHHostWarningBeforeEngineAndOnlyForHostMaster(t *testing.T) {
	for _, host := range []bool{false, true} {
		_, terminal := testTerminal(t)
		profile := ""
		var output bytes.Buffer
		sentinel := errors.New("engine reached")
		cmd := sshCommand(func(*cobra.Command) (*app.Engine, error) {
			if strings.Contains(output.String(), hostMasterWarning) != host {
				t.Error("warning missing, late, or emitted in container mode", output.String())
			}
			return nil, sentinel
		}, &profile)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetIn(terminal)
		cmd.SetErr(&output)
		args := []string{".", "staging"}
		if host {
			args = append(args, "--host-master")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	}
}

func TestSSHInsecureFlagReachesContainerMaster(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		t.Run(strconv.FormatBool(insecure), func(t *testing.T) {
			e, _, target := namedCLIFixture(t)
			_, terminal := testTerminal(t)
			failure := errors.New("fixture authentication failure")
			var captured []string
			e.Docker.Runner.(*dockertest.Daemon).Attached = func(_ context.Context, command docker.Command) error {
				i := slices.Index(command.Args, sshshare.Supervisor)
				if i < 0 {
					return nil
				}
				captured = command.Args[i+3:]
				return failure
			}
			profile := ""
			cmd := sshCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &profile)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetIn(terminal)
			cmd.SetErr(new(bytes.Buffer))
			args := []string{target, "staging"}
			if insecure {
				args = append(args, "--insecure")
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			want := []string{"staging"}
			if insecure {
				want = append(want, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "GlobalKnownHostsFile=/dev/null")
			}
			if !slices.Equal(captured, want) {
				t.Fatalf("master options: got %v, want %v", captured, want)
			}
		})
	}
}

func TestSSHRequiresTerminalAndHasNoCredentialCopyFlags(t *testing.T) {
	for _, args := range [][]string{{".", "staging"}, {".", "staging", "-i", "some-key"}, {".", "staging", "--identity", "some-key"}, {".", "staging", "--detach"}, {".", "host\nHost *"}} {
		profile := ""
		cmd := sshCommand(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid command initialized state")
			return nil, nil
		}, &profile)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted", args)
		}
	}
}

func TestSSHWarningUsesTerminalColorOptOuts(t *testing.T) {
	enableTerminalColors(t)
	render := func(file *os.File) error {
		profile := ""
		sentinel := errors.New("stop")
		cmd := sshCommand(func(*cobra.Command) (*app.Engine, error) { return nil, sentinel }, &profile)
		cmd.SetIn(file)
		cmd.SetErr(file)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs([]string{".", "staging", "--host-master"})
		if err := cmd.Execute(); !errors.Is(err, sentinel) {
			return err
		}
		return nil
	}
	if output := terminalOutput(t, 80, render); !strings.Contains(output, "\x1b[33m"+hostMasterWarning) {
		t.Fatal(output)
	}
	t.Setenv("NO_COLOR", "1")
	if output := terminalOutput(t, 80, render); strings.Contains(output, "\x1b[") || !strings.Contains(output, hostMasterWarning) {
		t.Fatal(output)
	}
}

func TestSSHExitCodeIsPreserved(t *testing.T) {
	cmd := &cobra.Command{Use: "ssh"}
	cmd.SetErr(new(bytes.Buffer))
	if code := RenderError(cmd, errors.Join(&sshshare.ExitError{Code: 255}, &docker.ExitError{Code: 9, Operation: "cleanup"})); code != 255 {
		t.Fatal(code)
	}
}
