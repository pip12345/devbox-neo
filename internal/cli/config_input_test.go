package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/cliui"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestConfigValidationRetainsInputAndExplicitCorrectionSaves(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	input := &workflowInput{lines: []string{"1\n", "not a network\n", "host\n"}}
	input.before = func(step int) {
		if step == 2 {
			if !strings.Contains(out.String(), "Current value: not a network") || !strings.Contains(out.String(), "Not saved:") {
				t.Fatal("rejected text was lost", out.String())
			}
			source, err := s.ConfigSource(owner)
			if err != nil || source["network"] != nil {
				t.Fatal("invalid input saved", source, err)
			}
		}
	}
	m := testMenu(context.Background(), input, &out)
	source, _ := s.ConfigSource(owner)
	changed := false
	if err := editConfigField(m, s, owner, resource.ConfigField{Key: "network", Kind: "string"}, source, &changed); err != nil || !changed {
		t.Fatal(out.String(), err)
	}
	source, _ = s.ConfigSource(owner)
	if string(source["network"]) != `"host"` {
		t.Fatal(source)
	}
}

func TestFailedConfigWriteRetainsValueAndReleasesLockBeforeRetry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary filesystem permission enforcement")
	}
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(owner.Root, 0700) })
	var out bytes.Buffer
	input := &workflowInput{lines: []string{"1\n", "host\n", "host\n"}}
	input.before = func(step int) {
		if step == 1 {
			if err := os.Chmod(owner.Root, 0500); err != nil {
				t.Fatal(err)
			}
		}
		if step == 2 {
			if !strings.Contains(out.String(), "Current value: host") || !strings.Contains(out.String(), "Not saved:") {
				t.Fatal("failed write lost pending input", out.String())
			}
			source, err := s.ConfigSource(owner)
			if err != nil || source["network"] != nil {
				t.Fatal("failed write saved input", source, err)
			}
			if err := os.Chmod(owner.Root, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := s.SetConfigField(ctx, owner, "base_image", source["base_image"], json.RawMessage(`"debian:bookworm-slim"`), false); err != nil {
				t.Fatal("input held the config owner lock", err)
			}
		}
	}
	m := testMenu(context.Background(), input, &out)
	source, _ := s.ConfigSource(owner)
	changed := false
	if err := editConfigField(m, s, owner, resource.ConfigField{Key: "network", Kind: "string"}, source, &changed); err != nil || !changed {
		t.Fatal(err, out.String())
	}
	source, _ = s.ConfigSource(owner)
	if string(source["network"]) != `"host"` || source["base_image"] == nil {
		t.Fatal("retry lost value or unrelated edit", source)
	}
}

func TestNativeConfigEditStartsFromLiteralSourceAndRetainsRejectedText(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INPUT_NETWORK", "default")
	if err := s.SetConfigField(context.Background(), owner, "network", nil, json.RawMessage(`"${env:INPUT_NETWORK}"`), false); err != nil {
		t.Fatal(err)
	}
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "devbox-neo"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		source, err := s.ConfigSource(owner)
		if err != nil {
			return err
		}
		return editConfigField(m, s, owner, resource.ConfigField{Key: "network", Kind: "string"}, source, new(bool))
	})
	p.wait("Edit value")
	p.send("\r")
	p.wait("Enter a value")
	if !strings.Contains(p.output(), "${env:INPUT_NETWORK}") {
		t.Fatal("prefill flattened the source expression", p.output())
	}
	p.send("\x15not a network\r")
	p.wait("Not saved:")
	p.send("\x15host\r")
	p.finish(done)
	source, _ := s.ConfigSource(owner)
	if string(source["network"]) != `"host"` {
		t.Fatal(source)
	}
}

func TestPlainSensitiveInputDisablesEchoAndRestoresTerminal(t *testing.T) {
	p := newTerminalProbe(t)
	t.Setenv("TERM", "dumb")
	original, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		r := cliui.New(ctx, tty, tty)
		defer func() { err = errors.Join(err, r.Finish()) }()
		value, accepted, err := r.Text(cliui.TextRequest{Prompt: "Enter secret: ", Initial: "old-private-value", Sensitive: true})
		if err == nil && (!accepted || value != "new-private-value") {
			return errors.New("wrong sensitive submission")
		}
		return err
	})
	p.wait("Enter secret:")
	quiet, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || quiet.Lflag&unix.ECHO != 0 {
		t.Fatal("secret input still echoed", err)
	}
	p.send("new-private-value\n")
	p.finish(done)
	if strings.Contains(p.output(), "private-value") {
		t.Fatal("secret leaked", p.output())
	}
	after, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || *after != *original {
		t.Fatal("secret prompt did not restore terminal", err)
	}
}

func TestPlainSensitiveCancellationRestoresTerminal(t *testing.T) {
	p := newTerminalProbe(t)
	t.Setenv("TERM", "dumb")
	original, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cancelInput := make(chan context.CancelFunc, 1)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		cancelInput <- cancel
		r := cliui.New(ctx, tty, tty)
		defer func() { err = errors.Join(err, r.Finish()) }()
		_, accepted, err := r.Text(cliui.TextRequest{Prompt: "Secret to cancel: ", Initial: "private-value", Sensitive: true})
		if !accepted && errors.Is(err, context.Canceled) {
			return nil
		}
		return errors.New("sensitive input did not cancel")
	})
	p.wait("Secret to cancel:")
	(<-cancelInput)()
	p.finish(done)
	after, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || *after != *original {
		t.Fatal("cancellation retained no-echo mode", err)
	}
	if strings.Contains(p.output(), "private-value") {
		t.Fatal("cancelled input leaked its initial value")
	}
}

func TestSensitiveListPrefillNeverUsesRedactedDisplayValue(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"version":1,"env":["TOKEN=private-value"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "devbox-neo"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		return editList(m, s, owner, resource.ConfigField{Key: "env", Kind: "list", Sensitive: true}, new(bool))
	})
	p.wait("Add environment variable")
	p.send("/Edit environment\r\r")
	p.wait("Select environment variable")
	p.send("\r")
	p.wait("Enter a value")
	p.send("\r")
	p.wait("Saved Environment variables")
	p.send("qq")
	p.finish(done)
	if strings.Contains(p.output(), "private-value") {
		t.Fatal("prefill exposed secret", p.output())
	}
	source, _ := s.ConfigSource(owner)
	var entries []string
	if err := json.Unmarshal(source["env"], &entries); err != nil || len(entries) != 1 || entries[0] != "TOKEN=private-value" {
		t.Fatal("redacted data was saved", err)
	}
}
