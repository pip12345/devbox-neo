package resource

import (
	"encoding/json"
	"path/filepath"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/harness"
)

type ConfigView struct {
	Scope      string                         `json:"scope"`
	Path       string                         `json:"path"`
	Values     map[string]any                 `json:"values"`
	Trace      artifact.Trace                 `json:"trace"`
	References map[string]map[string][]string `json:"host_references,omitempty"`
	Harness    map[string]string              `json:"harness,omitempty"`
}

func fields(value any) (map[string]any, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(b, &result)
	return result, err
}
func (s Service) ShowGlobal() (ConfigView, error) {
	path := filepath.Join(s.Home, "config.json")
	g, err := config.ReadGlobal(path, config.Snapshot())
	if err != nil {
		return ConfigView{}, err
	}
	values, err := fields(g)
	if err != nil {
		return ConfigView{}, err
	}
	values["global_env"] = config.RedactEnv(g.GlobalEnv)
	trace := artifact.Trace{Layers: []artifact.Layer{{Name: "built-in default"}, {Name: "global", Path: path}}, Sources: map[string][]string{}, EntrySources: map[string][]string{}}
	for range g.GlobalEnv {
		trace.EntrySources["global_env"] = append(trace.EntrySources["global_env"], "global")
	}
	for key := range values {
		trace.Sources[key] = []string{"built-in default"}
	}
	var raw map[string]any
	if err = config.Decode(g.Raw, &raw); err != nil {
		return ConfigView{}, err
	}
	for key := range raw {
		trace.Sources[key] = []string{"global"}
	}
	return ConfigView{Scope: "global", Path: path, Values: values, Trace: trace, References: map[string]map[string][]string{path: g.References}}, nil
}
func (s Service) ShowProfile(name string) (ConfigView, error) {
	owner, err := s.Profile(name)
	if err != nil {
		return ConfigView{}, err
	}
	resolved, err := artifact.ResolveWithHost(s.Home, "", name, config.Layer{}, config.Snapshot())
	if err != nil {
		return ConfigView{}, err
	}
	return s.configView("profile", filepath.Join(owner.Root, "config.json"), resolved)
}
func (s Service) ShowProject(folder, profile string) (ConfigView, error) {
	identity, err := environment.Identify(folder, "", true)
	if err != nil {
		return ConfigView{}, err
	}
	resolved, err := artifact.PreviewSelection(s.Home, identity.Workspace, artifact.Selection{Profile: profile, IgnoreProject: s.IgnoreProject}, config.Layer{}, nil, config.Snapshot())
	if err != nil {
		return ConfigView{}, err
	}
	return s.configView("project", filepath.Join(identity.Workspace, ".devbox/config.json"), resolved)
}
func (s Service) configView(scope, path string, r artifact.Resolved) (ConfigView, error) {
	values, err := fields(r.Settings)
	if err != nil {
		return ConfigView{}, err
	}
	values["env"] = config.RedactEnv(r.Settings.Env)
	result := ConfigView{Scope: scope, Path: path, Values: values, Trace: r.Trace, References: map[string]map[string][]string{filepath.Join(s.Home, "config.json"): r.Global.References}}
	for _, layer := range r.Layers {
		if len(layer.Config.References) > 0 {
			result.References[filepath.Join(layer.Path, "config.json")] = layer.Config.References
		}
	}
	if r.Settings.Harness != "" {
		selected, err := harness.Load(s.Home, r.Settings.Harness)
		if err != nil {
			return ConfigView{}, err
		}
		result.Harness = map[string]string{"name": selected.Definition.Name, "origin": selected.Origin}
	}
	return result, nil
}
