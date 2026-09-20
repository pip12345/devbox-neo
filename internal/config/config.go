// Package config owns the strict current-format schemas and sparse merge rules.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"devbox/internal/commanderror"
)

type Global struct {
	Raw            []byte              `json:"-"`
	References     map[string][]string `json:"-"`
	EnvInputs      []EnvInput          `json:"-"`
	Version        int                 `json:"version"`
	DefaultProfile string              `json:"default_profile"`
	DefaultHarness string              `json:"default_harness"`
	GlobalEnv      []string            `json:"global_env"`
	IgnoreProject  bool                `json:"ignore_project"`
}
type VSCode struct {
	Extensions []string `json:"extensions,omitempty"`
}
type Layer struct {
	Raw         []byte              `json:"-"`
	References  map[string][]string `json:"-"`
	EnvInputs   []EnvInput          `json:"-"`
	Version     int                 `json:"version"`
	Shell       *[]string           `json:"shell,omitempty"`
	Harness     *string             `json:"harness,omitempty"`
	Network     *string             `json:"network,omitempty"`
	HarnessArgs []string            `json:"harness_args,omitempty"`
	DockerArgs  []string            `json:"docker_args,omitempty"`
	Mounts      []string            `json:"mounts,omitempty"`
	Env         []string            `json:"env,omitempty"`
	Ports       []string            `json:"ports,omitempty"`
	VSCode      VSCode              `json:"vscode,omitempty"`
	Inherit     *bool               `json:"inherit,omitempty"`
	BaseImage   *string             `json:"base_image,omitempty"`
}
type Settings struct {
	EnvInputs   []EnvInput `json:"-"`
	Shell       []string   `json:"shell"`
	Harness     string     `json:"harness"`
	BaseImage   string     `json:"base_image"`
	Network     string     `json:"network"`
	HarnessArgs []string   `json:"harness_args"`
	DockerArgs  []string   `json:"docker_args"`
	Mounts      []string   `json:"mounts"`
	Env         []string   `json:"-"`
	Ports       []string   `json:"ports"`
	VSCode      VSCode     `json:"vscode"`
}

func Defaults() Settings {
	return Settings{Shell: []string{"bash"}, Network: "default", BaseImage: "debian:bookworm-slim"}
}

var Name = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
var EnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var NetworkName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Decode also rejects duplicate object keys: accepting the last occurrence would
// give validators and other JSON tools different views of the same configuration.
func Decode(b []byte, v any) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if err := value(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	return nil
}
func value(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			t, err := d.Token()
			if err != nil {
				return err
			}
			k, ok := t.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if seen[k] {
				return fmt.Errorf("duplicate JSON field")
			}
			seen[k] = true
			if err = value(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = value(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}
func ReadGlobal(path string, host Host) (g Global, err error) {
	defer func() { err = configurationError(path, err) }()
	g = Global{Version: 1}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	raw, err := ParseGlobal(b)
	if err != nil {
		return g, fmt.Errorf("%s: invalid global config: %w", path, err)
	}
	expanded, refs, err := Expand(b, path, host)
	if err != nil {
		return g, err
	}
	g, err = ParseGlobal(expanded)
	if err != nil {
		return g, fmt.Errorf("%s: invalid expanded global config: %w", path, err)
	}
	g.Raw = b
	g.References = refs
	g.EnvInputs, err = envInputs(raw.GlobalEnv, g.GlobalEnv, path, "global_env", host, true)
	if err != nil {
		return g, err
	}
	g.GlobalEnv = nil
	for _, input := range g.EnvInputs {
		g.GlobalEnv = append(g.GlobalEnv, input.Value)
	}
	if g.DefaultProfile != "" && !Name.MatchString(g.DefaultProfile) {
		return g, fmt.Errorf("%s: invalid default_profile", path)
	}
	return g, nil
}
func ReadLayer(path string, host Host) (Layer, error) {
	l := Layer{Version: 1}
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	return ResolveLayer(b, path, host)
}
func ResolveLayer(b []byte, path string, host Host) (l Layer, err error) {
	defer func() { err = configurationError(path, err) }()
	l = Layer{Version: 1}
	raw, err := ParseLayer(b)
	if err != nil {
		return l, fmt.Errorf("%s: invalid layer: %w", path, err)
	}
	expanded, refs, err := Expand(b, path, host)
	if err != nil {
		return l, err
	}
	l, err = ParseLayer(expanded)
	if err != nil {
		return l, fmt.Errorf("%s: invalid expanded layer: %w", path, err)
	}
	l.Raw = b
	l.References = refs
	l.EnvInputs, err = envInputs(raw.Env, l.Env, path, "env", host, false)
	return l, err
}

// ReadLayer deliberately returns missing-file errors unchanged: participation
// and fresh-state decisions belong to its callers, before public diagnostics.
func configurationError(path string, err error) error {
	if err == nil {
		return nil
	}
	var actionable *commanderror.Error
	if errors.As(err, &actionable) {
		return err
	}
	code, message := "invalid_configuration", "Invalid configuration: "+err.Error()
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		code, message = "configuration_unavailable", "Cannot read configuration: "+pathError.Err.Error()
	}
	return commanderror.New(code, message, path, err)
}

// Source operations validate shape without resolving values. Copying a profile
// or editing one field must preserve expressions, not flatten host/global inputs.
func ParseLayer(b []byte) (Layer, error) {
	l := Layer{Version: 1, Raw: b}
	if err := Decode(b, &l); err != nil {
		return l, err
	}
	if l.Version != 1 {
		return l, fmt.Errorf("unsupported version")
	}
	if l.BaseImage != nil && !strings.Contains(*l.BaseImage, "${env:") && !ImageReference.MatchString(*l.BaseImage) {
		return l, fmt.Errorf("invalid base_image reference")
	}
	if l.HarnessArgs != nil && (l.Harness == nil || *l.Harness == "") {
		return l, fmt.Errorf("harness_args requires a harness in the same configuration layer")
	}
	return l, nil
}
func ParseGlobal(b []byte) (Global, error) {
	g := Global{Version: 1, Raw: b}
	if err := Decode(b, &g); err != nil {
		return g, err
	}
	if g.Version != 1 {
		return g, fmt.Errorf("unsupported version")
	}
	return g, nil
}
func (s *Settings) Apply(l Layer) {
	if l.BaseImage != nil {
		s.BaseImage = *l.BaseImage
	}
	if l.Shell != nil {
		s.Shell = append([]string(nil), (*l.Shell)...)
	}
	if l.Harness != nil {
		s.Harness = *l.Harness
	}
	if l.Network != nil {
		s.Network = *l.Network
	}
	s.HarnessArgs = append(s.HarnessArgs, l.HarnessArgs...)
	s.DockerArgs = append(s.DockerArgs, l.DockerArgs...)
	s.Mounts = append(s.Mounts, l.Mounts...)
	s.Env = append(s.Env, l.Env...)
	s.EnvInputs = append(s.EnvInputs, l.EnvInputs...)
	s.Ports = append(s.Ports, l.Ports...)
	s.VSCode.Extensions = append(s.VSCode.Extensions, l.VSCode.Extensions...)
}
func (s Settings) Validate() error {
	if err := s.ValidateFields(); err != nil {
		return err
	}
	if s.Harness == "" {
		return commanderror.New("harness_required", "No harness selected.", "", nil)
	}
	return nil
}
func (s Settings) ValidateFields() error {
	if len(s.Shell) == 0 || s.Shell[0] == "" {
		return fmt.Errorf("shell must be non-empty argv")
	}
	if s.Harness != "" && !Name.MatchString(s.Harness) {
		return fmt.Errorf("invalid harness name")
	}
	if s.BaseImage != "" && !ImageReference.MatchString(s.BaseImage) {
		return fmt.Errorf("invalid base_image reference")
	}
	if !NetworkName.MatchString(s.Network) {
		return fmt.Errorf("network must be a valid Docker network name")
	}
	if s.Network == "host" && len(s.Ports) > 0 {
		return fmt.Errorf("host networking cannot publish ports")
	}
	return nil
}
