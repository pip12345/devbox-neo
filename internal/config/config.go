// Package config owns the strict current-format schemas and sparse merge rules.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

type Global struct {
	Version        int      `json:"version"`
	DefaultProfile string   `json:"default_profile"`
	DefaultHarness string   `json:"default_harness"`
	GlobalEnv      []string `json:"global_env"`
	IgnoreProject  bool     `json:"ignore_project_overrides"`
}
type VSCode struct {
	Extensions []string `json:"extensions,omitempty"`
}
type Layer struct {
	Version        int       `json:"version"`
	OnExit         *string   `json:"on_exit,omitempty"`
	Shell          *[]string `json:"default_shell,omitempty"`
	Harness        *string   `json:"harness,omitempty"`
	Network        *string   `json:"network,omitempty"`
	HarnessArgs    []string  `json:"harness_args,omitempty"`
	DockerArgs     []string  `json:"docker_args,omitempty"`
	Mounts         []string  `json:"extra_mounts,omitempty"`
	Env            []string  `json:"extra_env,omitempty"`
	Ports          []string  `json:"extra_ports,omitempty"`
	VSCode         VSCode    `json:"vscode,omitempty"`
	InheritProfile *bool     `json:"inherit_profile,omitempty"`
}
type Settings struct {
	OnExit      string   `json:"on_exit"`
	Shell       []string `json:"default_shell"`
	Harness     string   `json:"harness"`
	Network     string   `json:"network"`
	HarnessArgs []string `json:"harness_args"`
	DockerArgs  []string `json:"docker_args"`
	Mounts      []string `json:"extra_mounts"`
	Env         []string `json:"-"`
	Ports       []string `json:"extra_ports"`
	VSCode      VSCode   `json:"vscode"`
}

func Defaults() Settings {
	return Settings{OnExit: "stop", Shell: []string{"bash"}, Network: "default"}
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
func ReadGlobal(path string) (Global, error) {
	g := Global{Version: 1}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	if bytes.Contains(b, []byte("${env:")) {
		return g, fmt.Errorf("%s: host substitution awaits phase 3; expressions are not used as literal values", path)
	}
	g, err = ParseGlobal(b)
	if err != nil {
		return g, fmt.Errorf("%s: invalid global config: %w", path, err)
	}
	if g.DefaultProfile != "" && !Name.MatchString(g.DefaultProfile) {
		return g, fmt.Errorf("%s: invalid default_profile", path)
	}
	return g, nil
}
func ReadLayer(path string, project bool) (Layer, error) {
	l := Layer{Version: 1}
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	if bytes.Contains(b, []byte("${env:")) {
		return l, fmt.Errorf("%s: host substitution awaits phase 3; expressions are not used as literal values", path)
	}
	l, err = ParseLayer(b, project)
	if err != nil {
		return l, fmt.Errorf("%s: invalid layer: %w", path, err)
	}
	return l, nil
}

// Source operations validate shape without resolving values. Copying a profile
// or editing one field must preserve expressions, not flatten host/global inputs.
func ParseLayer(b []byte, project bool) (Layer, error) {
	l := Layer{Version: 1}
	if err := Decode(b, &l); err != nil {
		return l, err
	}
	if l.Version != 1 {
		return l, fmt.Errorf("unsupported version")
	}
	if !project && l.InheritProfile != nil {
		return l, fmt.Errorf("inherit_profile is project-only")
	}
	return l, nil
}
func ParseGlobal(b []byte) (Global, error) {
	g := Global{Version: 1}
	if err := Decode(b, &g); err != nil {
		return g, err
	}
	if g.Version != 1 {
		return g, fmt.Errorf("unsupported version")
	}
	return g, nil
}
func (s *Settings) Apply(l Layer) {
	if l.OnExit != nil {
		s.OnExit = *l.OnExit
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
	s.Ports = append(s.Ports, l.Ports...)
	s.VSCode.Extensions = append(s.VSCode.Extensions, l.VSCode.Extensions...)
}
func (s Settings) Validate() error {
	if s.OnExit != "stop" && s.OnExit != "running" {
		return fmt.Errorf("on_exit must be stop or running")
	}
	if len(s.Shell) == 0 || s.Shell[0] == "" {
		return fmt.Errorf("default_shell must be non-empty argv")
	}
	if s.Harness == "" {
		return fmt.Errorf("no harness selected; use devbox-neo profile init <name> --harness <name> or devbox-neo project init <folder> --harness <name>")
	}
	if !Name.MatchString(s.Harness) {
		return fmt.Errorf("invalid harness name")
	}
	if !NetworkName.MatchString(s.Network) {
		return fmt.Errorf("network must be a valid Docker network name")
	}
	if s.Network == "host" && len(s.Ports) > 0 {
		return fmt.Errorf("host networking cannot publish ports")
	}
	return nil
}
