package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOpenCodeBackendStartup(t *testing.T) {
	for _, tc := range []struct {
		name, marker, message string
		starts, statuses      int
	}{
		{name: "cold", starts: 1, statuses: 1},
		{name: "bind-race", marker: "collide-once", starts: 2, statuses: 1},
		{name: "collisions-exhausted", marker: "collide-always", message: "three port collisions", starts: 3, statuses: 1},
		{name: "startup-error", marker: "fail-start", message: "Could not start the local backend (exit 1)", starts: 1, statuses: 1},
		{name: "unrelated-collision", marker: "other-port-collision", message: "Could not start the local backend (exit 1)", starts: 1, statuses: 1},
		{name: "startup-timeout", marker: "timeout-start", message: "Local backend startup timed out", starts: 1, statuses: 1},
		{name: "settings-error", marker: "fail-settings", message: "Could not inspect local backend settings"},
		{name: "invalid-settings", marker: "invalid-settings", message: "Invalid local backend settings"},
		{name: "status-error", marker: "fail-status", message: "Could not inspect the local backend", statuses: 1},
		{name: "invalid-status", marker: "invalid-status", message: "Invalid local backend status", statuses: 1},
		{name: "paths-error", marker: "fail-paths", message: "Could not locate local backend settings", statuses: 1},
		{name: "rejected-port", marker: "reject-port", message: "Native backend settings rejected the selected port", statuses: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := authTestFixture(t)
			if tc.marker != "" {
				authWrite(t, filepath.Join(f.data, tc.marker), "")
			}
			output, err := f.command().CombinedOutput()
			if bytes.Contains(output, []byte("dont-print-synthetic-secret")) {
				t.Fatal("native backend diagnostics escaped")
			}
			if tc.message == "" {
				if err != nil {
					t.Fatalf("backend startup: %v: %s", err, output)
				}
			} else {
				if err == nil || !strings.Contains(string(output), tc.message) {
					t.Fatalf("backend failure: %v: %s", err, output)
				}
				for _, name := range []string{"auth-calls", "started"} {
					if _, err := os.Stat(filepath.Join(f.data, name)); !os.IsNotExist(err) {
						t.Fatal("auth or foreground client ran after backend failure", name, err)
					}
				}
			}
			calls, err := os.ReadFile(filepath.Join(f.data, "backend-calls"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(calls), "disabled\n") != 1 || strings.Count(string(calls), "status\n") != tc.statuses || strings.Count(string(calls), "start\n") != tc.starts || strings.Contains(string(calls), "set\n") {
				t.Fatalf("unexpected backend operations: %s", calls)
			}
			if tc.starts > 0 {
				ports, err := os.ReadFile(filepath.Join(f.data, "backend-ports"))
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range strings.Fields(string(ports)) {
					port, err := strconv.Atoi(value)
					if err != nil || port < 1 || port > 65535 {
						t.Fatal("invalid selected backend port", value, err)
					}
				}
			}
		})
	}
}

func TestOpenCodeBackendReusesHealthyService(t *testing.T) {
	f := authTestFixture(t)
	authWrite(t, filepath.Join(f.data, "config", "service.json"), `{"port":54321}`)
	authWrite(t, filepath.Join(f.data, "backend-running"), "")
	for range 2 {
		if output, err := f.command().CombinedOutput(); err != nil {
			t.Fatalf("healthy backend: %v: %s", err, output)
		}
	}
	calls, err := os.ReadFile(filepath.Join(f.data, "backend-calls"))
	if err != nil || string(calls) != "disabled\nstatus\ndisabled\nstatus\n" {
		t.Fatal("healthy backend was reconfigured", string(calls), err)
	}
	port, err := os.ReadFile(filepath.Join(f.data, "config", "service.json"))
	if err != nil || string(port) != `{"port":54321}` {
		t.Fatal("healthy backend port changed", string(port), err)
	}
}

func backendPortChooser(t *testing.T, f authFixture, script string) string {
	t.Helper()
	tools := filepath.Join(f.root, "tools")
	if err := os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tools, "node")
	authWrite(t, path, "#!/bin/bash\n"+script)
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + tools + ":" + os.Getenv("PATH")
}

func TestOpenCodeBackendPortSelectionFailure(t *testing.T) {
	for _, tc := range []struct{ name, script, message string }{
		{"command-error", "echo dont-print-synthetic-secret >&2; exit 1\n", "Could not select a local backend port"},
		{"invalid-output", "echo dont-print-synthetic-secret\n", "Invalid local backend port"},
		{"out-of-range", "echo 65536\n", "Invalid local backend port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := authTestFixture(t)
			cmd := f.command()
			cmd.Env = append(cmd.Env, backendPortChooser(t, f, tc.script))
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.message) || bytes.Contains(output, []byte("dont-print-synthetic-secret")) {
				t.Fatalf("port selection failure: %v: %s", err, output)
			}
			calls, err := os.ReadFile(filepath.Join(f.data, "backend-calls"))
			if err != nil || string(calls) != "disabled\nstatus\npaths\n" {
				t.Fatal("backend configured after port selection failure", string(calls), err)
			}
		})
	}
}

func TestOpenCodeBackendConfigPreservesNativeSettings(t *testing.T) {
	f := authTestFixture(t)
	path := filepath.Join(f.data, "config", "service.json")
	original := `{"password":"dont-print-synthetic-secret","hostname":"127.0.0.1","cors":["http://localhost:1234"],"env":{"KEEP":"kept"},"unrelated":{"keep":true}}`
	authWrite(t, path, original)
	if output, err := f.command().CombinedOutput(); err != nil {
		t.Fatalf("backend configuration: %v: %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(original), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	port, ok := after["port"].(float64)
	if !ok || port < 1 || port > 65535 {
		t.Fatal("selected port was not saved")
	}
	delete(after, "port")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unrelated native settings changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("native settings are not private", err)
	}
}

func TestOpenCodeBackendConfigFailure(t *testing.T) {
	for _, mode := range []string{"invalid-json", "non-object", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := authTestFixture(t)
			path := filepath.Join(f.data, "config", "service.json")
			switch mode {
			case "invalid-json":
				authWrite(t, path, "dont-print-synthetic-secret")
			case "non-object":
				authWrite(t, path, "[]")
			case "symlink":
				target := filepath.Join(f.root, "native-settings.json")
				authWrite(t, target, "{}")
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			output, err := f.command().CombinedOutput()
			if err == nil || !strings.Contains(string(output), "Could not configure the local backend port") || bytes.Contains(output, []byte("dont-print-synthetic-secret")) {
				t.Fatalf("configuration failure: %v: %s", err, output)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid configuration changed", err)
			}
			remaining, err := filepath.Glob(filepath.Join(f.data, "config", ".devbox-service.*"))
			if err != nil || len(remaining) != 0 {
				t.Fatal("private settings temporary file was retained", remaining, err)
			}
			calls, err := os.ReadFile(filepath.Join(f.data, "backend-calls"))
			if err != nil || strings.Contains(string(calls), "start\n") || strings.Contains(string(calls), "set\n") {
				t.Fatal("configuration failure started or stopped a backend", string(calls), err)
			}
		})
	}
}

func TestOpenCodeBackendDisabled(t *testing.T) {
	f := authTestFixture(t)
	path := filepath.Join(f.data, "config", "service.json")
	original := `{"disabled":true,"password":"dont-print-synthetic-secret"}`
	authWrite(t, path, original)
	authWrite(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	if output, err := f.command().CombinedOutput(); err != nil {
		t.Fatalf("disabled backend: %v: %s", err, output)
	}
	calls, err := os.ReadFile(filepath.Join(f.data, "backend-calls"))
	if err != nil || string(calls) != "disabled\n" {
		t.Fatal("managed backend was accessed while disabled", string(calls), err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != original {
		t.Fatal("disabled backend settings changed", err)
	}
	assertAuth(t, filepath.Join(f.data, "pulled"), sharedAuth)
}

func TestOpenCodeBackendFailurePreservesPendingAuth(t *testing.T) {
	f := authTestFixture(t)
	authWrite(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	authWrite(t, filepath.Join(f.data, "update"), refreshedAuth)
	authWrite(t, filepath.Join(f.data, "fail-push"), "")
	if _, err := f.command().CombinedOutput(); err == nil {
		t.Fatal("expected failed write-back")
	}
	if err := os.Remove(filepath.Join(f.data, "backend-running")); err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(f.data, "fail-start"), "")
	output, err := f.command().CombinedOutput()
	if err == nil || !strings.Contains(string(output), "Could not start the local backend") {
		t.Fatalf("backend startup failure: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(f.store(t), "pending", "baseline.sorted"), sharedAuth)
	assertAuth(t, filepath.Join(f.data, "dbstate"), refreshedAuth)
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), sharedAuth)
	for _, name := range []string{"fail-start", "fail-push", "fail-export", "update"} {
		if err := os.Remove(filepath.Join(f.data, name)); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := f.command().CombinedOutput(); err != nil {
		t.Fatalf("backend and auth recovery: %v: %s", err, output)
	}
	assertAuth(t, filepath.Join(f.shared, "credentials.json"), refreshedAuth)
}

func TestOpenCodeBackendAdministrationBypassesSynchronization(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"service", "get", "port"}} {
		f := authTestFixture(t)
		if output, err := f.command(args...).CombinedOutput(); err != nil {
			t.Fatalf("native administration: %v: %s", err, output)
		}
		if _, err := os.Stat(f.store(t)); !os.IsNotExist(err) {
			t.Fatal("native administration entered shared auth synchronization", err)
		}
	}
}

type nativeAuthFixture struct {
	authFixture
	env []string
}

func newNativeAuthFixture(t *testing.T) nativeAuthFixture {
	t.Helper()
	binary := os.Getenv("DEVBOX_OPENCODE_NATIVE")
	if binary == "" {
		t.Skip("set DEVBOX_OPENCODE_NATIVE to verify the native CLI with isolated synthetic auth")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	f := nativeAuthFixture{authFixture: authTestFixture(t)}
	f.native = binary
	f.data = filepath.Join(f.root, "data", "opencode")
	if err := os.Mkdir(f.data, 0700); err != nil {
		t.Fatal(err)
	}
	for _, assignment := range os.Environ() {
		if !strings.HasPrefix(assignment, "OPENCODE_") && !strings.HasPrefix(assignment, "XDG_") && !strings.HasPrefix(assignment, "HOME=") {
			f.env = append(f.env, assignment)
		}
	}
	for key, dir := range map[string]string{"HOME": "home", "XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state"} {
		path := filepath.Join(f.root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.env = append(f.env, key+"="+path)
	}
	f.env = append(f.env, "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_FILEWATCHER=1", `OPENCODE_CONFIG_CONTENT={"update":"disable"}`)
	t.Cleanup(func() {
		if output, err := f.nativeOutput("service", "stop"); err != nil {
			t.Errorf("native service cleanup: %v: %s", err, output)
		}
	})
	return f
}

func (f nativeAuthFixture) nativeOutput(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.native, args...)
	cmd.Env, cmd.Dir = f.env, f.root
	return cmd.CombinedOutput()
}

func (f nativeAuthFixture) assertExport(t *testing.T, desired string) {
	t.Helper()
	authWrite(t, filepath.Join(f.shared, "credentials.json"), desired)
	cmd := f.command("auth", "export")
	cmd.Env, cmd.Dir = f.env, f.root
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

func TestOpenCodeAuthNativeCommands(t *testing.T) {
	f := newNativeAuthFixture(t)
	// Holding the native default port reproduces host-network contention without
	// Docker. No test-selected port is configured before invoking the wrapper.
	listener, err := net.Listen("tcp", "127.0.0.1:49374")
	if err == nil {
		t.Cleanup(func() { _ = listener.Close() })
	} else if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatal(err)
	}
	for _, desired := range []string{sharedAuth, refreshedAuth, "[]"} {
		f.assertExport(t, desired)
	}
}

func TestOpenCodeBackendNativeBindRace(t *testing.T) {
	f := newNativeAuthFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	chooser := backendPortChooser(t, f.authFixture, `if [[ ! -e $TEST_PORT_PROBED ]]; then
    touch "$TEST_PORT_PROBED"
    echo "$TEST_OCCUPIED_PORT"
else
    exec "$TEST_NODE" "$@"
fi
`)
	f.env = append(f.env, chooser,
		"TEST_PORT_PROBED="+filepath.Join(f.root, "probed"),
		"TEST_OCCUPIED_PORT="+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
		"TEST_NODE="+node)
	f.assertExport(t, sharedAuth)
}

func TestOpenCodeBackendNativeDisabled(t *testing.T) {
	f := newNativeAuthFixture(t)
	if output, err := f.nativeOutput("service", "set", "disabled", "true"); err != nil {
		t.Fatalf("disable native service: %v: %s", err, output)
	}
	for _, desired := range []string{sharedAuth, refreshedAuth} {
		f.assertExport(t, desired)
		if output, err := f.nativeOutput("service", "status"); err != nil || string(output) != "stopped\n" {
			t.Fatalf("disabled native backend started: %v: %s", err, output)
		}
	}
	if output, err := f.nativeOutput("service", "get", "port"); err != nil || string(output) != "\n" {
		t.Fatal("disabled native backend was assigned a port", err)
	}
}

func TestOpenCodeBackendNativeStaleRegistration(t *testing.T) {
	f := newNativeAuthFixture(t)
	configDir := filepath.Join(f.root, "custom config")
	f.env = append(f.env, "OPENCODE_CONFIG_DIR="+configDir)
	// The stale registration simulates PID reuse after a restart. Only this
	// disposable process may be referenced, never a process outside the fixture.
	victim := exec.Command("sleep", "90")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = victim.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = victim.Process.Kill()
		<-exited
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if output, err := f.nativeOutput("debug", "paths", "config"); err != nil || strings.TrimSpace(string(output)) != configDir {
		t.Fatalf("native config directory: %v: %s", err, output)
	}
	settings, err := json.Marshal(map[string]any{"port": port, "password": "synthetic-native-password", "env": map[string]string{"KEEP_SETTING": "kept"}})
	if err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(configDir, "service.json"), string(settings))
	state, err := f.nativeOutput("debug", "paths", "state")
	if err != nil {
		t.Fatal(err)
	}
	registration, err := json.Marshal(map[string]any{
		"id": "synthetic-stale-registration", "pid": victim.Process.Pid,
		"url": "http://127.0.0.1:" + strconv.Itoa(port), "version": "2.2.0", "password": "synthetic-native-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(strings.TrimSpace(string(state)), "service.json"), string(registration))
	f.assertExport(t, sharedAuth)
	select {
	case <-exited:
		t.Fatal("cold-start configuration terminated an unrelated process")
	default:
	}
	after, err := os.ReadFile(filepath.Join(configDir, "service.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	if got["password"] != "synthetic-native-password" || !reflect.DeepEqual(got["env"], map[string]any{"KEEP_SETTING": "kept"}) {
		t.Fatal("native settings were not preserved")
	}
}

func TestOpenCodeBackendNativeSeparateStores(t *testing.T) {
	a, b := newNativeAuthFixture(t), newNativeAuthFixture(t)
	b.shared = a.shared
	a.assertExport(t, sharedAuth)
	b.assertExport(t, sharedAuth)
	endpoint := func(f nativeAuthFixture) string {
		t.Helper()
		output, err := f.nativeOutput("service", "status")
		if err != nil || !strings.HasPrefix(string(output), "http://127.0.0.1:") {
			t.Fatalf("native backend status: %v: %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	first, second := endpoint(a), endpoint(b)
	if first == second {
		t.Fatal("separate stores shared a backend endpoint")
	}
	a.assertExport(t, sharedAuth)
	if endpoint(a) != first || endpoint(b) != second {
		t.Fatal("reopening replaced a healthy backend")
	}
}
