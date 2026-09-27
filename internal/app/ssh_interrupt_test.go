package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/sshshare"
)

func TestSSHInterruptNormalizationOnlySuppressesExpectedRunnerResults(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	other := errors.New("unrelated failure")
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		err     error
		success bool
	}{
		{"container ctrl-c", context.Background(), commanderror.New("docker_command_failed", "interrupted", "", &docker.ExitError{Code: 130, Operation: "exec"}), true},
		{"host ctrl-c", context.Background(), &sshshare.ExitError{Code: 130}, true},
		{"host cancellation", cancelled, errors.Join(&sshshare.ExitError{Code: 143}, context.Canceled), true},
		{"docker cancellation", cancelled, errors.Join(&docker.ExitError{Code: 137, Operation: "exec"}, context.Canceled), true},
		{"context cancellation", cancelled, context.Canceled, true},
		{"login failure", context.Background(), &sshshare.ExitError{Code: 255}, false},
		{"login failure races cancellation", cancelled, errors.Join(&sshshare.ExitError{Code: 255}, context.Canceled), false},
		{"other error races cancellation", cancelled, errors.Join(&sshshare.ExitError{Code: 143}, other), false},
		{"external kill", context.Background(), &docker.ExitError{Code: 137, Operation: "exec"}, false},
		{"deadline", deadline, errors.Join(&sshshare.ExitError{Code: 143}, context.DeadlineExceeded), false},
		{"unrelated docker operation", context.Background(), &docker.ExitError{Code: 130, Operation: "stop"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := sshRunResult(tt.ctx, tt.err)
			if (err == nil) != tt.success {
				t.Fatal(err)
			}
			if errors.Is(tt.err, other) && !errors.Is(err, other) {
				t.Fatal("lost real failure")
			}
		})
	}
}

func TestSSHContainerCtrlCBeforeAuthenticationCleansUpNormally(t *testing.T) {
	for _, cleanupFails := range []bool{false, true} {
		e, d, q := fixture(t)
		ctx := context.Background()
		result, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		cleanupFailure := errors.New("container stop cleanup failed")
		d.Attached = func(_ context.Context, c docker.Command) error {
			if slices.Contains(c.Args, sshshare.Supervisor) {
				return &docker.ExitError{Code: 130, Operation: "exec"}
			}
			return nil
		}
		if cleanupFails {
			d.Fail = func(args []string) error {
				if args[0] == "stop" {
					return cleanupFailure
				}
				return nil
			}
		}
		err = e.SSH(ctx, result.SessionID, "", "staging", SSHOptions{})
		if cleanupFails {
			if !errors.Is(err, cleanupFailure) || strings.Contains(err.Error(), "exit 130") {
				t.Fatal("cleanup failure lost or interrupt reported", err)
			}
		} else if err != nil {
			t.Fatal("Ctrl-C reported as error", err)
		}
		files, _ := filepath.Glob(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, sshshare.RelativeRoot, "c", "*", "config"))
		if len(files) != 0 {
			t.Fatal("still published", files)
		}
		l, err := e.Store.Lock(ctx, sessionRecord(t, e, result.SessionID).Directory, sessionRecord(t, e, result.SessionID).ID)
		if err != nil {
			t.Fatal(err)
		}
		leases, err := l.Active()
		l.Close()
		if err != nil || len(leases) != 0 {
			t.Fatal("lease remained", leases, err)
		}
	}
}
