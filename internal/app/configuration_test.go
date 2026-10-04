package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestRecreateResolvesCurrentEnvironmentWithoutPersistingValues(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	t.Setenv("DEVBOX_TEST_TOKEN", "sentinel-secret")
	file := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, file, `{"version":1,"harness":"pi","env":["TOKEN=${env:DEVBOX_TEST_TOKEN}","OTHER=literal"]}`)
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "session.json")
	data := getFile(t, path)
	if bytes.Contains(data, []byte("sentinel-secret")) || bytes.Contains(data, []byte("OTHER=literal")) {
		t.Fatal("session persisted env values")
	}
	before := sessionRecord(t, e, result.SessionID)
	forgetSession(t, e, result.SessionID)
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal("explicit recreation failed", err)
	}
	forgetSession(t, e, result.SessionID)
	t.Setenv("DEVBOX_TEST_TOKEN", "new-secret")
	creates := count(d, "create")
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal("current env did not rebuild", err)
	}
	if count(d, "create") != creates+1 {
		t.Fatal("runtime was not rebuilt")
	}
	if after := sessionRecord(t, e, result.SessionID); after.ID != before.ID || after.Applied.Fingerprints.Container == before.Applied.Fingerprints.Container {
		t.Fatal("current env baseline not committed")
	}
	if data := getFile(t, path); bytes.Contains(data, []byte("new-secret")) {
		t.Fatal("current env persisted")
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
	record := sessionRecord(t, e, result.SessionID)
	if len(record.Applied.Creation.Ports) != 1 || len(record.Applied.Creation.RawArgs) != 1 || !strings.Contains(record.Applied.Creation.Metadata, "example.extension") {
		t.Fatal("creation options were dropped")
	}
	found := false
	for _, mount := range record.Applied.Creation.Mounts {
		if mount.Target == "/data" {
			found = mount.Source == source && mount.ReadOnly
		}
	}
	if !found {
		t.Fatal("extra mount was not normalized")
	}
	b := getFile(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "session.json"))
	if bytes.Contains(b, []byte("changed-after-resolution")) {
		t.Fatal("env value leaked")
	}
}
func TestInvalidCreationOptionsFailBeforeDocker(t *testing.T) {
	for body, want := range map[string]string{
		`{"harness":"pi","ports":["99999:80"]}`:               "ports:",
		`{"harness":"pi","network":"host","ports":["80:80"]}`: "host networking cannot publish ports",
		`{"harness":"pi","docker_args":["--name=foreign"]}`:   "option --name is owned by Devbox",
		`{"harness":"pi","docker_args":["--memory","4g"]}`:    "require --option=value",
		`{"harness":"pi","env":["DEVBOX_HOST=bad"]}`:          "DEVBOX_* environment is reserved",
	} {
		t.Run(body, func(t *testing.T) {
			e, d, q := fixture(t)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), body)
			if _, err := e.Create(context.Background(), q); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("creation did not reach the intended validator", want, err)
			}
			if len(d.History()) != 0 {
				t.Fatal("invalid inputs reached Docker")
			}
		})
	}
}
func TestRecreateUsesCurrentSelectedSources(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","env":["TOKEN=committed-value"]}`)
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, result.SessionID)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.json"), `{"harness":"pi","env":["TOKEN=desired-value"]}`)
	refs := []config.Reference{{Label: "replacement", Kind: config.ReferenceFixed, Path: dir}}
	if _, err := e.UpdateSources(ctx, before, refs); err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, result.SessionID)
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal("recreation did not resolve current selected sources", err)
	}
	after := sessionRecord(t, e, result.SessionID)
	if after.Applied.Inputs.Sources[0].Path != dir || after.Settings.Sources[0].Path != dir || after.Applied.Fingerprints.Container == before.Applied.Fingerprints.Container {
		t.Fatal("rebuild did not apply current selected container configuration")
	}
	data := getFile(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "session.json"))
	if bytes.Contains(data, []byte("committed-value")) || bytes.Contains(data, []byte("desired-value")) {
		t.Fatal("source change persisted environment values")
	}
}
func TestFileAndVolumeMountRecreationChecksKinds(t *testing.T) {
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
	forgetSession(t, e, result.SessionID)
	if _, err = e.Recreate(context.Background(), q, false); err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, result.SessionID)
	delete(d.Volumes, "shared-volume")
	if _, err = e.Recreate(context.Background(), q, false); err == nil {
		t.Fatal("missing volume silently recreated")
	}
}
