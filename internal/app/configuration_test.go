package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/config"
)

func TestConfigEnvironmentRecoveryNeverStoresOrAdoptsValues(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	t.Setenv("DEVBOX_TEST_TOKEN", "sentinel-secret")
	file := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, file, `{"version":1,"harness":"pi","extra_env":["TOKEN=${env:DEVBOX_TEST_TOKEN}","OTHER=literal"]}`)
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "sessions", result.Name, "session.json")
	data := getFile(t, path)
	if bytes.Contains(data, []byte("sentinel-secret")) || bytes.Contains(data, []byte("OTHER=literal")) {
		t.Fatal("session persisted env values")
	}
	before := record(t, e, result.Name)
	d.Forget(result.Name)
	if _, err = e.Start(ctx, result.Name, ""); err != nil {
		t.Fatal("verified env did not recover", err)
	}
	d.Forget(result.Name)
	t.Setenv("DEVBOX_TEST_TOKEN", "new-secret")
	creates := count(d, "create")
	var recoveryError *commanderror.Error
	if _, err = e.Start(ctx, result.Name, ""); !errors.As(err, &recoveryError) || recoveryError.Code != "recovery_unavailable" || len(recoveryError.Next) != 1 {
		t.Fatal("changed env was adopted", err)
	}
	if count(d, "create") != creates {
		t.Fatal("failed env verification mutated Docker")
	}
	if record(t, e, result.Name).Applied != before.Applied {
		t.Fatal("failed recovery advanced fingerprints")
	}
}
func TestPublicSubstitutionsAndCreationOptionsUseOneSnapshot(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	source := t.TempDir()
	file := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, file, `{"version":1,"harness":"${env:AGENT}","network":"${env:NET}","extra_env":["KEY=${env:SECRET}"],"extra_mounts":["${env:DATA}:/data:ro"],"extra_ports":["127.0.0.1:8080:80"],"docker_args":["--memory=256m"],"vscode":{"extensions":["example.extension"]}}`)
	q.Host = config.Host{"AGENT": "pi", "NET": "default", "SECRET": "snapshot-value", "DATA": source}
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	q.Host["SECRET"] = "changed-after-resolution"
	if !strings.Contains(strings.Join(spec.Env(), "\n"), "KEY=snapshot-value") {
		t.Fatal("resolved values reread host state")
	}
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	record := record(t, e, result.Name)
	if len(record.Creation.Ports) != 1 || len(record.Creation.RawArgs) != 1 || !strings.Contains(record.Creation.Metadata, "example.extension") {
		t.Fatal("creation options were dropped")
	}
	found := false
	for _, mount := range record.Creation.Mounts {
		if mount.Target == "/data" {
			found = mount.Source == source && mount.ReadOnly
		}
	}
	if !found {
		t.Fatal("extra mount was not normalized")
	}
	b := getFile(t, filepath.Join(e.Store.Home, "sessions", result.Name, "session.json"))
	if bytes.Contains(b, []byte("changed-after-resolution")) {
		t.Fatal("env value leaked")
	}
}
func TestInvalidCreationOptionsFailBeforeDocker(t *testing.T) {
	for _, body := range []string{
		`{"harness":"pi","extra_ports":["99999:80"]}`,
		`{"harness":"pi","network":"host","extra_ports":["80:80"]}`,
		`{"harness":"pi","docker_args":["--name=foreign"]}`,
		`{"harness":"pi","docker_args":["--memory","4g"]}`,
		`{"harness":"pi","extra_env":["DEVBOX_HOST=bad"]}`,
	} {
		t.Run(body, func(t *testing.T) {
			e, d, q := fixture(t)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), body)
			if _, err := e.Open(context.Background(), q); err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if len(d.History()) != 0 {
				t.Fatal("invalid inputs reached Docker")
			}
		})
	}
}
func TestCLIOnlyEnvironmentRequiresExplicitRecreation(t *testing.T) {
	e, d, q := fixture(t)
	q.Overrides.Env = []string{"TOKEN=invocation-secret"}
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	data := getFile(t, filepath.Join(e.Store.Home, "sessions", result.Name, "session.json"))
	if bytes.Contains(data, []byte("invocation-secret")) {
		t.Fatal("invocation env persisted")
	}
	d.Forget(result.Name)
	if _, err = e.Start(context.Background(), result.Name, ""); err == nil {
		t.Fatal("recovery guessed invocation-only environment")
	}
}
func TestFileAndVolumeMountRecoveryChecksKinds(t *testing.T) {
	e, d, q := fixture(t)
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("data"), 0600)
	q.Overrides.Mounts = []string{file + ":/extra-file", "shared-volume:/shared"}
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	d.Forget(result.Name)
	if _, err = e.Start(context.Background(), result.Name, ""); err != nil {
		t.Fatal(err)
	}
	d.Forget(result.Name)
	delete(d.Volumes, "shared-volume")
	if _, err = e.Start(context.Background(), result.Name, ""); err == nil {
		t.Fatal("missing volume silently recreated")
	}
}
