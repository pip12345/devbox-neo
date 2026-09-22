package resource

import (
	"encoding/json"
	"path/filepath"

	"devbox/internal/artifact"
	"devbox/internal/config"
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
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}

// The directory editor displays this source over built-in defaults, never the
// merged configuration of an arbitrary session that happens to reference it.
func (s Service) ShowOwner(o Owner) (ConfigView, error) {
	resolved, err := artifact.Resolve([]config.Source{{Label: o.Name, Path: o.Root}}, config.Snapshot())
	if err != nil {
		return ConfigView{}, err
	}
	return s.ConfigurationView("config", filepath.Join(o.Root, "config.json"), resolved)
}

func (s Service) ConfigurationView(scope, path string, r artifact.Resolved) (ConfigView, error) {
	values, err := fields(r.Settings)
	if err != nil {
		return ConfigView{}, err
	}
	values["env"] = config.RedactEnv(r.Settings.Env)
	result := ConfigView{Scope: scope, Path: path, Values: values, Trace: r.Trace, References: map[string]map[string][]string{}}
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
