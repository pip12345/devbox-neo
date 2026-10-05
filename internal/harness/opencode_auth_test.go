package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fakeOpenCode = `#!/bin/bash
set -eu
case ${1-}:${2-} in
    auth:export)
        if [[ -e $FAKE_DATA/fail-export ]]; then echo 'dont-print-synthetic-secret' >&2; exit 1; fi
        jq . "$FAKE_DATA/dbstate" ;;
    auth:import) cp -- "$3" "$FAKE_DATA/dbstate" ;;
    api:DELETE)
        if [[ -e $FAKE_DATA/fail-delete ]]; then echo 'dont-print-synthetic-secret' >&2; exit 1; fi
        jq --arg id "${3##*/}" '[.[] | select(.id != $id)]' "$FAKE_DATA/dbstate" >"$FAKE_DATA/new"
        mv -- "$FAKE_DATA/new" "$FAKE_DATA/dbstate" ;;
    --version:) echo 'test version' ;;
    service:*) printf '%s\n' "$@" ;;
    *)
        cp -- "$FAKE_DATA/dbstate" "$FAKE_DATA/pulled"
        printf '%s\n' "$@" >"$FAKE_DATA/argv"
        [[ ! -e $FAKE_DATA/update ]] || cp -- "$FAKE_DATA/update" "$FAKE_DATA/dbstate"
        [[ ! -e $FAKE_DATA/fail-push ]] || touch "$FAKE_DATA/fail-export"
        touch "$FAKE_DATA/started"
        while [[ -e $FAKE_DATA/wait ]]; do sleep 0.05; done
        exit "${FAKE_EXIT:-0}" ;;
esac
`

type authFixture struct {
	root, shared, data, script, native string
}

func authTestFixture(t *testing.T) authFixture {
	t.Helper()
	for _, tool := range []string{"bash", "flock", "jq", "timeout", "sync"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("OpenCode wrapper tests require " + tool)
		}
	}
	f := authFixture{root: t.TempDir()}
	f.shared, f.data = filepath.Join(f.root, "shared"), filepath.Join(f.root, "data")
	for _, dir := range []string{f.shared, f.data} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	h, err := Load(t.TempDir(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	f.script, f.native = filepath.Join(f.root, "sync.sh"), filepath.Join(f.root, "native")
	authWrite(t, f.script, string(h.InstallFiles["opencode-auth.sh"].Data))
	authWrite(t, f.native, fakeOpenCode)
	if err := os.Chmod(f.native, 0700); err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(f.data, "dbstate"), "[]")
	return f
}

func authWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f authFixture) store(t *testing.T) string {
	t.Helper()
	info, err := os.Stat(f.data)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	return filepath.Join(f.shared, fmt.Sprintf("store-%d-%d", stat.Dev, stat.Ino))
}

func (f authFixture) command(args ...string) *exec.Cmd {
	cmd := exec.Command("bash", append([]string{f.script, f.native, f.shared, f.data}, args...)...)
	cmd.Env = append(os.Environ(), "FAKE_DATA="+f.data)
	return cmd
}

func canonicalAuth(t *testing.T, data []byte) string {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(entries, func(a, b map[string]any) int { return strings.Compare(a["id"].(string), b["id"].(string)) })
	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertAuth(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalAuth(t, data) != canonicalAuth(t, []byte(want)) {
		t.Fatal("credential snapshot did not match")
	}
}

const sharedAuth = `[{"id":"cred_a","integrationID":"provider_a","label":"a","active":true,"value":{"type":"key","key":"synthetic-current"}},{"id":"cred_b","integrationID":"provider_a","label":"b","active":false,"value":{"type":"key","key":"synthetic-other"}}]`
const refreshedAuth = `[{"id":"cred_a","integrationID":"provider_a","label":"a","active":true,"value":{"type":"oauth","methodID":"test","access":"synthetic-access","refresh":"synthetic-refresh","expires":1000,"metadata":{"account":"test"}}}]`

func TestOpenCodeAuthPullPushAndExitStatus(t *testing.T) {
	f := authTestFixture(t)
	authWrite(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(f.data, "dbstate"), refreshedAuth)
	authWrite(t, filepath.Join(f.data, "update"), refreshedAuth)
	authWrite(t, filepath.Join(f.data, "history"), "untouched")
	cmd := f.command("run", "prompt with spaces", "--", "--help")
	cmd.Env = append(cmd.Env, "FAKE_EXIT=7")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("foreground status changed: %v, %s", err, output)
	}
	assertAuth(t, filepath.Join(f.data, "pulled"), sharedAuth)
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), refreshedAuth)
	args, _ := os.ReadFile(filepath.Join(f.data, "argv"))
	if string(args) != "run\nprompt with spaces\n--\n--help\n" {
		t.Fatal("launch arguments changed")
	}
	if history, _ := os.ReadFile(filepath.Join(f.data, "history")); string(history) != "untouched" {
		t.Fatal("history changed")
	}
	if _, err := os.Stat(filepath.Join(f.store(t), "pending")); !os.IsNotExist(err) {
		t.Fatal("write-back did not finish", err)
	}
	for _, name := range []string{"credentials.json", "lock"} {
		info, err := os.Stat(filepath.Join(f.shared, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("auth permissions", name, err)
		}
	}
}

func TestOpenCodeAuthMissingSnapshotDoesNotAdoptLocalCredentials(t *testing.T) {
	f := authTestFixture(t)
	authWrite(t, filepath.Join(f.data, "dbstate"), sharedAuth)
	if output, err := f.command().CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	assertAuth(t, filepath.Join(f.data, "pulled"), "[]")
	if _, err := os.Stat(filepath.Join(f.shared, "credentials.json")); !os.IsNotExist(err) {
		t.Fatal("unchanged auth was needlessly published", err)
	}
}

func TestOpenCodeAuthPullFailureDoesNotLaunchOrPublish(t *testing.T) {
	for _, mode := range []string{"invalid", "delete", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := authTestFixture(t)
			snapshot := filepath.Join(f.shared, "credentials.json")
			authWrite(t, snapshot, sharedAuth)
			authWrite(t, filepath.Join(f.data, "dbstate"), refreshedAuth)
			switch mode {
			case "invalid":
				authWrite(t, snapshot, "dont-print-synthetic-secret")
			case "delete":
				authWrite(t, filepath.Join(f.data, "fail-delete"), "")
			case "symlink":
				if err := os.Remove(snapshot); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.root, "absent"), snapshot); err != nil {
					t.Fatal(err)
				}
			}
			output, err := f.command().CombinedOutput()
			if err == nil || bytes.Contains(output, []byte("dont-print-synthetic-secret")) {
				t.Fatal("pull failure was hidden or sensitive output escaped", err)
			}
			if _, err := os.Stat(filepath.Join(f.data, "started")); !os.IsNotExist(err) {
				t.Fatal("client launched after failed pull")
			}
			if mode == "delete" {
				assertAuth(t, snapshot, sharedAuth)
			}
		})
	}
}

func TestOpenCodeAuthWritebackFailureAndOriginalStoreRecovery(t *testing.T) {
	f := authTestFixture(t)
	authWrite(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(f.data, "update"), refreshedAuth)
	authWrite(t, filepath.Join(f.data, "fail-push"), "")
	output, err := f.command().CombinedOutput()
	if err == nil || bytes.Contains(output, []byte("dont-print-synthetic-secret")) {
		t.Fatal("write-back failure was hidden or sensitive output escaped", err)
	}
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	other := authTestFixture(t)
	other.shared = f.shared
	if output, err := other.command().CombinedOutput(); err != nil {
		t.Fatalf("pending write-back blocked another session: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	for _, name := range []string{"fail-push", "fail-export", "update"} {
		if err := os.Remove(filepath.Join(f.data, name)); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := f.command().CombinedOutput(); err != nil {
		t.Fatalf("recovery: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(f.data, "pulled"), refreshedAuth)
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), refreshedAuth)
}

func TestOpenCodeAuthSameStoreLockAndTerminalSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			f := authTestFixture(t)
			authWrite(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
			authWrite(t, filepath.Join(f.data, "update"), refreshedAuth)
			authWrite(t, filepath.Join(f.data, "wait"), "")
			cmd := f.command()
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					_ = cmd.Wait()
				}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(f.data, "started")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("client did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if output, err := f.command().CombinedOutput(); err == nil || !bytes.Contains(output, []byte("Already in use")) {
				t.Fatal("concurrent import into the same database was not rejected", err)
			}
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			waited = true
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 128+int(sig) {
				t.Fatalf("signal status: %v: %s", err, output.String())
			}
			assertAuth(t, filepath.Join(f.shared, "credentials.json"), refreshedAuth)
		})
	}
}

type authProcess struct {
	cmd    *exec.Cmd
	output bytes.Buffer
	waited bool
}

func startAuthProcess(t *testing.T, f authFixture) *authProcess {
	t.Helper()
	authWrite(t, filepath.Join(f.data, "wait"), "")
	p := &authProcess{cmd: f.command()}
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p.cmd.Stdout, p.cmd.Stderr = &p.output, &p.output
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !p.waited {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			_ = p.cmd.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.data, "started")); err == nil {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatal("parallel auth client did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func finishAuthProcess(t *testing.T, f authFixture, p *authProcess, code int) {
	t.Helper()
	if err := os.Remove(filepath.Join(f.data, "wait")); err != nil {
		t.Fatal(err)
	}
	err := p.cmd.Wait()
	p.waited = true
	if p.cmd.ProcessState.ExitCode() != code {
		t.Fatalf("auth client exit: %v: %s", err, p.output.String())
	}
}

func TestOpenCodeAuthParallelSnapshotsAndConflictCopies(t *testing.T) {
	for _, mode := range []string{"unchanged", "changed", "same-result"} {
		t.Run(mode, func(t *testing.T) {
			a := authTestFixture(t)
			b := authTestFixture(t)
			b.shared = a.shared
			authWrite(t, filepath.Join(a.shared, "credentials.json"), sharedAuth)
			authWrite(t, filepath.Join(a.data, "update"), refreshedAuth)
			otherAuth := strings.ReplaceAll(sharedAuth, "synthetic-current", "synthetic-second")
			if mode == "changed" {
				authWrite(t, filepath.Join(b.data, "update"), otherAuth)
			} else if mode == "same-result" {
				authWrite(t, filepath.Join(b.data, "update"), refreshedAuth)
			}
			first := startAuthProcess(t, a)
			second := startAuthProcess(t, b)
			assertAuth(t, filepath.Join(b.data, "pulled"), sharedAuth)
			// Starting B while A is active proves the shared lock is not run-long.
			finishAuthProcess(t, a, first, 0)
			assertAuth(t, filepath.Join(a.shared, "credentials.json"), refreshedAuth)
			wantCode := 0
			if mode == "changed" {
				wantCode = 1
			}
			finishAuthProcess(t, b, second, wantCode)
			assertAuth(t, filepath.Join(a.shared, "credentials.json"), refreshedAuth)
			conflicts, err := filepath.Glob(filepath.Join(b.store(t), "conflict.*"))
			if err != nil {
				t.Fatal(err)
			}
			if mode != "changed" {
				if len(conflicts) != 0 {
					t.Fatal("unchanged or identical credentials caused a false conflict")
				}
				return
			}
			if len(conflicts) != 1 || !strings.Contains(second.output.String(), "shared auth was not overwritten") {
				t.Fatal("conflicting credentials were not retained and reported")
			}
			assertAuth(t, conflicts[0], otherAuth)
			info, err := os.Stat(conflicts[0])
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("recovery copy permissions", err)
			}
			if err := os.Remove(filepath.Join(b.data, "update")); err != nil {
				t.Fatal(err)
			}
			if output, err := b.command().CombinedOutput(); err != nil {
				t.Fatalf("conflict prevented a later launch: %v: %s", err, output)
			}
			assertAuth(t, filepath.Join(b.data, "pulled"), refreshedAuth)
			assertAuth(t, conflicts[0], otherAuth)
		})
	}
}

func TestOpenCodeAuthConcurrentCommitHasOneWinner(t *testing.T) {
	a := authTestFixture(t)
	b := authTestFixture(t)
	b.shared = a.shared
	authWrite(t, filepath.Join(a.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(a.data, "update"), refreshedAuth)
	otherAuth := strings.ReplaceAll(sharedAuth, "synthetic-current", "synthetic-second")
	authWrite(t, filepath.Join(b.data, "update"), otherAuth)
	first := startAuthProcess(t, a)
	second := startAuthProcess(t, b)
	for _, f := range []authFixture{a, b} {
		if err := os.Remove(filepath.Join(f.data, "wait")); err != nil {
			t.Fatal(err)
		}
	}
	_ = first.cmd.Wait()
	first.waited = true
	_ = second.cmd.Wait()
	second.waited = true
	firstCode, secondCode := first.cmd.ProcessState.ExitCode(), second.cmd.ProcessState.ExitCode()
	if !((firstCode == 0 && secondCode == 1) || (firstCode == 1 && secondCode == 0)) {
		t.Fatal("concurrent writers did not report exactly one winner", firstCode, secondCode)
	}
	winner, loser, rejected := refreshedAuth, b, otherAuth
	if secondCode == 0 {
		winner, loser, rejected = otherAuth, a, refreshedAuth
	}
	assertAuth(t, filepath.Join(a.shared, "credentials.json"), winner)
	conflicts, _ := filepath.Glob(filepath.Join(loser.store(t), "conflict.*"))
	if len(conflicts) != 1 {
		t.Fatal("losing write-back was not retained")
	}
	assertAuth(t, conflicts[0], rejected)
}

func TestOpenCodeAuthPendingRecoveryDoesNotOverwriteNewerSharedAuth(t *testing.T) {
	a := authTestFixture(t)
	b := authTestFixture(t)
	b.shared = a.shared
	authWrite(t, filepath.Join(a.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(a.data, "update"), refreshedAuth)
	authWrite(t, filepath.Join(a.data, "fail-push"), "")
	if _, err := a.command().CombinedOutput(); err == nil {
		t.Fatal("expected pending write-back")
	}
	otherAuth := strings.ReplaceAll(sharedAuth, "synthetic-current", "synthetic-second")
	authWrite(t, filepath.Join(b.data, "update"), otherAuth)
	if output, err := b.command().CombinedOutput(); err != nil {
		t.Fatalf("other session could not commit: %v: %s", err, output)
	}
	for _, name := range []string{"fail-push", "fail-export", "update"} {
		if err := os.Remove(filepath.Join(a.data, name)); err != nil {
			t.Fatal(err)
		}
	}
	output, err := a.command().CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("Recovery copy:")) {
		t.Fatalf("pending conflict recovery: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(a.shared, "credentials.json"), otherAuth)
	assertAuth(t, filepath.Join(a.data, "pulled"), otherAuth)
	conflicts, _ := filepath.Glob(filepath.Join(a.store(t), "conflict.*"))
	if len(conflicts) != 1 {
		t.Fatal("pending local changes were discarded")
	}
	assertAuth(t, conflicts[0], refreshedAuth)
}

func TestOpenCodeAuthKilledRunRecoveryUsesOriginalSnapshot(t *testing.T) {
	a := authTestFixture(t)
	b := authTestFixture(t)
	b.shared = a.shared
	authWrite(t, filepath.Join(a.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(a.data, "update"), refreshedAuth)
	p := startAuthProcess(t, a)
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = p.cmd.Wait()
	p.waited = true
	if err := os.Remove(filepath.Join(a.data, "wait")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(a.data, "update")); err != nil {
		t.Fatal(err)
	}
	if output, err := b.command().CombinedOutput(); err != nil {
		t.Fatalf("killed run blocked another store: %v: %s", err, output)
	}
	if output, err := a.command().CombinedOutput(); err != nil {
		t.Fatalf("killed run recovery: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(a.shared, "credentials.json"), refreshedAuth)
	assertAuth(t, filepath.Join(a.data, "pulled"), refreshedAuth)
}

func TestOpenCodeAuthNativeCommands(t *testing.T) {
	binary := os.Getenv("DEVBOX_OPENCODE_NATIVE")
	if binary == "" {
		t.Skip("set DEVBOX_OPENCODE_NATIVE to verify the native CLI with isolated synthetic auth")
	}
	f := authTestFixture(t)
	f.native = binary
	f.data = filepath.Join(f.root, "data", "opencode")
	if err := os.Mkdir(f.data, 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, assignment := range os.Environ() {
		if !strings.HasPrefix(assignment, "OPENCODE_") && !strings.HasPrefix(assignment, "XDG_") && !strings.HasPrefix(assignment, "HOME=") {
			env = append(env, assignment)
		}
	}
	for key, dir := range map[string]string{"HOME": "home", "XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state"} {
		path := filepath.Join(f.root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		env = append(env, key+"="+path)
	}
	env = append(env, "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_FILEWATCHER=1", `OPENCODE_CONFIG_CONTENT={"update":"disable"}`)
	native := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env, cmd.Dir = env, f.root
		return cmd.Run()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if err := native("service", "set", "port", strconv.Itoa(port)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native("service", "stop"); err != nil {
			t.Error(err)
		}
	})
	for _, desired := range []string{sharedAuth, refreshedAuth, "[]"} {
		authWrite(t, filepath.Join(f.shared, "credentials.json"), desired)
		cmd := f.command("auth", "export")
		cmd.Env, cmd.Dir = env, f.root
		var output, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("native auth commands: %v: %s", err, stderr.String())
		}
		if canonicalAuth(t, output.Bytes()) != canonicalAuth(t, []byte(desired)) {
			t.Fatal("native import/export changed credentials or active account selection")
		}
		assertAuth(t, filepath.Join(f.shared, "credentials.json"), desired)
	}
}
