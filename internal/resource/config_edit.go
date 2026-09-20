package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// These are source-editing controls, not a second schema or resolver. The config
// parsers remain authoritative for field types and scope restrictions.
func ConfigFields(scope string) []ConfigField {
	if scope == "global" {
		return []ConfigField{
			{Key: "default_profile", Kind: "string", Help: "Default named profile; reset for no default."},
			{Key: "default_harness", Kind: "string", Help: "Harness used when participating layers do not select one."},
			{Key: "global_env", Kind: "list", Help: "Environment entries for all containers: NAME or KEY=VALUE. Prefer host references; typed input is visible.", Sensitive: true},
			{Key: "ignore_project", Kind: "bool", Help: "Exclude project configuration and artifacts."},
		}
	}
	fields := []ConfigField{
		{Key: "harness", Kind: "string", Help: "Select a harness; reset to use inherited selection."},
		{Key: "network", Kind: "string", Help: "Primary network: default, host, or an existing Docker network name."},
		{Key: "shell", Kind: "list", Help: "Shell command followed by its arguments, one entry each. Replaces the inherited shell; the command cannot be empty."},
		{Key: "harness_args", Kind: "list", Help: "Arguments for the harness named in this config, one per entry. Matching harness layers append; other harness arguments are ignored."},
		{Key: "docker_args", Kind: "list", Help: "Docker options added by this config. Inherited options are kept. Use --option=value for options with values."},
		{Key: "mounts", Kind: "list", Help: "Mounts added by this config. Inherited mounts are kept. Format: SOURCE:/absolute/target[:options]."},
		{Key: "env", Kind: "list", Help: "KEY=VALUE entries added by this config. Inherited entries are kept. Prefer ${env:NAME}; typed input is visible.", Sensitive: true},
		{Key: "ports", Kind: "list", Help: "Port forwards added by this config. Inherited forwards are kept. Format: [HOST_IP:]HOST_PORT:CONTAINER_PORT."},
		{Key: "vscode", Kind: "extensions", Help: "VS Code extension IDs added by this config. Inherited extensions are kept."},
	}
	fields = append(fields,
		ConfigField{Key: "inherit", Kind: "bool", Help: "Include preceding configuration sources; false discards them entirely. Default true."},
		ConfigField{Key: "base_image", Kind: "string", Help: "Debian/Ubuntu-compatible upstream image; Devbox prepares the development user before customization."})
	return fields
}

func (s Service) ConfigOwner(scope, target string) (Owner, error) {
	switch scope {
	case "global":
		return Owner{Kind: "global", Root: s.Home}, nil
	case "profile":
		return s.Profile(target)
	case "project":
		return s.Project(target)
	default:
		return Owner{}, fmt.Errorf("unknown configuration scope")
	}
}

func (s Service) ConfigSource(o Owner) (map[string]json.RawMessage, error) {
	_, values, err := s.readConfigSource(o)
	return values, err
}

func (s Service) readConfigSource(o Owner) ([]byte, map[string]json.RawMessage, error) {
	var b []byte
	var err error
	if o.Kind == "global" {
		_, b, err = s.global()
	} else {
		b, _, err = readLayer(o)
	}
	if err != nil {
		return nil, nil, err
	}
	var values map[string]json.RawMessage
	err = config.Decode(b, &values)
	return b, values, err
}

// SetConfigField re-reads under the same owner lock used by init/default changes.
// Only the edited field is compared, so unrelated concurrent edits are retained.
// No resolved values or redacted display data are ever written back to source.
func (s Service) SetConfigField(ctx context.Context, o Owner, key string, expected, value json.RawMessage, remove bool) error {
	var field *ConfigField
	for _, f := range ConfigFields(o.Kind) {
		if f.Key == key {
			f := f
			field = &f
			break
		}
	}
	if field == nil {
		return fmt.Errorf("setting is not editable in this scope")
	}
	lockKey := o.Root
	if o.Kind == "global" {
		lockKey = filepath.Join(o.Root, "config.json")
	}
	lock, err := s.lock(ctx, lockKey)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(lock)
	b, current, err := s.readConfigSource(o)
	if err != nil {
		return err
	}
	if !sameConfigValue(current[key], expected) {
		return ErrConfigChanged
	}
	if !remove {
		if err = s.validateConfigField(o.Kind, *field, value); err != nil {
			return err
		}
	}
	data, err := patch(b, key, value, remove)
	if err != nil {
		return err
	}
	if o.Kind == "global" {
		_, err = config.ParseGlobal(data)
	} else {
		_, err = config.ParseLayer(data)
	}
	if err != nil {
		return fmt.Errorf("invalid configuration source: %w", err)
	}
	if bytes.Equal(b, data) {
		return nil
	}
	path, err := fsutil.Path(o.Root, "config.json")
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return fsutil.Write(path, data, 0600)
}

func sameConfigValue(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func (s Service) validateConfigField(scope string, field ConfigField, value json.RawMessage) error {
	invalid := fmt.Errorf("invalid %s value; %s", field.Key, field.Help)
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return invalid
	}
	data, err := patch([]byte(`{"version":1}`), field.Key, value, false)
	if err != nil {
		return invalid
	}
	if scope == "global" {
		_, err = config.ParseGlobal(data)
	} else {
		var layer config.Layer
		err = config.Decode(data, &layer)
	}
	if err != nil {
		return invalid
	}
	// Source edits can contain unresolved host references. Validate literal
	// values here; the normal effective resolver validates expanded values.
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
	case "harness", "default_harness":
		if text != "" {
			if _, err = harness.Load(s.Home, text); err != nil {
				return fmt.Errorf("select an available harness or reset to inherited selection")
			}
		}
	case "default_profile":
		if text != "" {
			o, err := s.Profile(text)
			if err != nil {
				return invalid
			}
			if _, _, err = readLayer(o); err != nil {
				return fmt.Errorf("select an existing valid profile or reset to no default")
			}
		}
	case "base_image":
		if !config.ImageReference.MatchString(text) {
			return invalid
		}
	case "network", "shell":
		layer, _ := config.ParseLayer(data)
		settings := config.Defaults()
		settings.Apply(layer)
		if settings.ValidateFields() != nil {
			return invalid
		}
	case "env", "global_env", "ports":
		var entries []string
		if err = json.Unmarshal(value, &entries); err != nil {
			return invalid
		}
		for _, entry := range entries {
			if field.Key == "ports" {
				err = docker.ValidatePort(entry)
			} else if field.Key == "global_env" && !strings.Contains(entry, "=") {
				if !config.EnvName.MatchString(entry) {
					return invalid
				}
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

func (s Service) ShowOwner(o Owner) (ConfigView, error) {
	switch o.Kind {
	case "global":
		return s.ShowGlobal()
	case "profile":
		return s.ShowProfile(o.Name)
	case "project":
		return s.ShowProject(o.Name, "")
	default:
		return ConfigView{}, os.ErrInvalid
	}
}
