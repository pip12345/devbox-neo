package migration

import (
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/environment"
)

func TestConversionUsesCurrentConfigNamesAndReportsRemovedPolicy(t *testing.T) {
	layer, notices, err := convertLayer([]byte(`{"harness":"pi","harness_args":["--version"],"extra_mounts":["data:/data"],"extra_ports":["8080:80"],"extra_env":["TOKEN=${env:TOKEN}"],"default_shell":["sh"],"on_exit":"running"}`))
	if err != nil {
		t.Fatal(err)
	}
	data := string(encode(layer))
	for _, key := range []string{`"mounts"`, `"ports"`, `"env"`, `"shell"`} {
		if !strings.Contains(data, key) {
			t.Fatal("missing destination key", key, data)
		}
	}
	for _, key := range []string{`"extra_mounts"`, `"extra_ports"`, `"extra_env"`, `"default_shell"`, `"on_exit"`} {
		if strings.Contains(data, key) {
			t.Fatal("retained source key", key, data)
		}
	}
	if !strings.Contains(strings.Join(notices, " "), "Removed on_exit") {
		t.Fatal(notices)
	}
	if _, err = config.ParseLayer([]byte(data), false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = convertLayer([]byte(`{"harness_args":["--unowned"]}`)); err == nil {
		t.Fatal("unowned source arguments were silently assigned")
	}
}

func TestReadOnlySourceSettingsRequireExplicitReview(t *testing.T) {
	var old oldMetadata
	if err := config.Decode([]byte(`{"creation_settings":{"read_only":true}}`), &old); err != nil {
		t.Fatal(err)
	}
	changes := changedSettings(old, environment.Spec{Settings: config.Defaults()})
	if !strings.Contains(strings.Join(changes, ", "), "removed workspace read_only") {
		t.Fatal(changes)
	}
}
