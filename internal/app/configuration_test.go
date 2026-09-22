package app

import (
	"bytes"
	"context"
	"encoding/json"
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
	write(t, file, `{"version":1,"harness":"pi","env":["TOKEN=${env:DEVBOX_TEST_TOKEN}","OTHER=literal"]}`)
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
	write(t, file, `{"version":1,"harness":"${env:AGENT}","network":"${env:NET}","env":["KEY=${env:SECRET}"],"mounts":["${env:DATA}:/data:ro"],"ports":["127.0.0.1:8080:80"],"docker_args":["--memory=256m"],"vscode":{"extensions":["example.extension"]}}`)
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
		`{"harness":"pi","ports":["99999:80"]}`,
		`{"harness":"pi","network":"host","ports":["80:80"]}`,
		`{"harness":"pi","docker_args":["--name=foreign"]}`,
		`{"harness":"pi","docker_args":["--memory","4g"]}`,
		`{"harness":"pi","env":["DEVBOX_HOST=bad"]}`,
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
func TestSourceEditsDoNotReplaceCommittedEnvironmentRecoveryInputs(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","env":["TOKEN=committed-value"]}`)
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := record(t, e, result.Name)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.json"), `{"harness":"pi","env":["TOKEN=desired-value"]}`)
	refs := []config.Reference{{Label: "replacement", Kind: config.ReferenceFixed, Path: dir}}
	if _, err := e.UpdateSources(ctx, before, refs); err != nil {
		t.Fatal(err)
	}
	d.Forget(result.Name)
	if _, err = e.Start(ctx, result.Name, ""); err != nil {
		t.Fatal("source edit invalidated committed recovery provenance", err)
	}
	after := record(t, e, result.Name)
	if after.Inputs.Sources[0].Path != before.Inputs.Sources[0].Path || after.Sources[0].Path != dir || after.Applied.Container != before.Applied.Container {
		t.Fatal("recovery applied desired container configuration instead of committed inputs")
	}
	data := getFile(t, filepath.Join(e.Store.Home, "sessions", result.Name, "session.json"))
	if bytes.Contains(data, []byte("committed-value")) || bytes.Contains(data, []byte("desired-value")) {
		t.Fatal("source change persisted environment values")
	}
}
func TestFileAndVolumeMountRecoveryChecksKinds(t *testing.T) {
	e, d, q := fixture(t)
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("data"), 0600)
	data, err := json.Marshal(config.Layer{Version: 1, Mounts: []string{file + ":/extra-file", "shared-volume:/shared"}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), string(data))
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
