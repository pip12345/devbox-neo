package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

var ErrConfigChanged = errors.New("this setting changed while the menu was open; review its current value and retry")

type ConfigField struct {
	Key, Kind, Help string
	Sensitive       bool
}

// These are source-editing controls, not a second schema or resolver.
func ConfigFields() []ConfigField {
	return []ConfigField{
		{Key: "harness", Kind: "string", Help: "Select a harness; remove this setting to leave the choice to other sources."},
		{Key: "network", Kind: "string", Help: "Primary network: default, host, or an existing Docker network name."},
		{Key: "shell", Kind: "list", Help: "Shell command followed by its arguments, one entry each. Replaces earlier shell argv; the command cannot be empty."},
		{Key: "harness_args", Kind: "list", Help: "Arguments for the harness named in this config, one per entry. Matching harness layers append; other harness arguments are ignored."},
		{Key: "docker_args", Kind: "list", Help: "Docker options added by this config. Earlier options are kept. Use --option=value for options with values."},
		{Key: "mounts", Kind: "list", Help: "Mounts added by this config. Earlier mounts are kept. Format: SOURCE:/absolute/target[:options]."},
		{Key: "env", Kind: "list", Help: "KEY=VALUE entries added by this config. Later assignments to the same variable win. Prefer ${env:NAME}; terminal input is masked.", Sensitive: true},
		{Key: "ports", Kind: "list", Help: "Port forwards added by this config. Earlier forwards are kept. Format: [HOST_IP:]HOST_PORT:CONTAINER_PORT."},
		{Key: "vscode", Kind: "extensions", Help: "VS Code extension IDs added by this config. Earlier extensions are kept."},
		{Key: "base_image", Kind: "string", Help: "Debian/Ubuntu-compatible upstream image; Devbox prepares the development user before customization."},
	}
}

func (s Service) ConfigSource(o Owner) (map[string]json.RawMessage, error) {
	_, values, err := s.readConfigSource(o)
	return values, err
}

func (s Service) readConfigSource(o Owner) ([]byte, map[string]json.RawMessage, error) {
	data, _, err := readLayer(o)
	if err != nil {
		return nil, nil, err
	}
	var values map[string]json.RawMessage
	err = config.Decode(data, &values)
	return data, values, err
}

// Only the edited field is compared under the shared owner lock. Unrelated
// concurrent edits are retained; resolved or redacted values are never saved.
func (s Service) SetConfigField(ctx context.Context, o Owner, key string, expected, value json.RawMessage, remove bool) error {
	var field *ConfigField
	for _, candidate := range ConfigFields() {
		if candidate.Key == key {
			field = &candidate
			break
		}
	}
	if field == nil {
		return fmt.Errorf("setting is not editable")
	}
	lock, err := s.lock(ctx, o.Root)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(lock)
	data, current, err := s.readConfigSource(o)
	if err != nil {
		return err
	}
	if !sameConfigValue(current[key], expected) {
		return ErrConfigChanged
	}
	if !remove {
		if err = s.validateConfigField(*field, value); err != nil {
			return err
		}
	}
	updated, err := patch(data, key, value, remove)
	if err != nil {
		return err
	}
	if _, err = config.ParseLayer(updated); err != nil {
		return fmt.Errorf("invalid configuration source: %w", err)
	}
	if bytes.Equal(data, updated) {
		return nil
	}
	path, err := fsutil.Path(o.Root, "config.json")
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return fsutil.Write(path, updated, 0600)
}

func sameConfigValue(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func (s Service) validateConfigField(field ConfigField, value json.RawMessage) error {
	invalid := fmt.Errorf("invalid %s value; %s", field.Key, field.Help)
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return invalid
	}
	data, err := patch([]byte(`{"version":1}`), field.Key, value, false)
	if err != nil {
		return invalid
	}
	var layer config.Layer
	if err := config.Decode(data, &layer); err != nil {
		return invalid
	}
	// Source edits preserve host expressions. Expanded values are validated by
	// the effective resolver, not flattened into shared files by the editor.
	if strings.Contains(string(value), "${env:") {
		return nil
	}
	var text string
	if field.Kind == "string" {
		if err = json.Unmarshal(value, &text); err != nil || strings.ContainsAny(text, "\x00\r\n") {
			return invalid
		}
	}
	switch field.Key {
	case "harness":
		if text != "" {
			if _, err = harness.Load(s.Home, text); err != nil {
				return fmt.Errorf("select an available harness or remove this setting")
			}
		}
	case "base_image":
		if !config.ImageReference.MatchString(text) {
			return invalid
		}
	case "network", "shell":
		settings := config.Defaults()
		settings.Apply(layer)
		if settings.ValidateFields() != nil {
			return invalid
		}
	case "env", "ports":
		var entries []string
		if err = json.Unmarshal(value, &entries); err != nil {
			return invalid
		}
		for _, entry := range entries {
			if field.Key == "ports" {
				err = docker.ValidatePort(entry)
			} else {
				err = config.ValidateEnvAssignment(entry)
			}
			if err != nil {
				return invalid
			}
		}
	}
	return nil
}
