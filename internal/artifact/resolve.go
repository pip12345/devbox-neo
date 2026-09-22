// Package artifact composes settings and artifacts from explicitly ordered sources.
package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

type Layer struct {
	Name   string       `json:"name"`
	Path   string       `json:"path"`
	Config config.Layer `json:"-"`
}
type Trace struct {
	Layers    []Layer             `json:"layers"`
	Artifacts map[string][]string `json:"artifacts"`
	Sources   map[string][]string `json:"sources"`
	// EntrySources follows the resolved list order, including duplicate values.
	EntrySources map[string][]string `json:"entry_sources,omitempty"`
}
type Resolved struct {
	Settings config.Settings
	Layers   []Layer
	Trace    Trace
	Sources  []config.Source
}

func Resolve(sources []config.Source, host config.Host) (Resolved, error) {
	return Preview(sources, nil, host)
}

// Preview accepts absolute directory inputs from the reference resolver. It is
// also used for isolated directory inspection, where a harness may be unset.
func Preview(sources []config.Source, proposed *SourcePreview, host config.Host) (r Resolved, err error) {
	defer func() {
		var actionable *commanderror.Error
		if err != nil && !errors.As(err, &actionable) {
			err = commanderror.New("invalid_configuration", "Invalid configuration: "+err.Error(), "", err)
		}
	}()
	if host == nil {
		host = config.Snapshot()
	}
	r = Resolved{Settings: config.Defaults(), Sources: append([]config.Source(nil), sources...), Trace: Trace{Artifacts: map[string][]string{}, Sources: map[string][]string{}, EntrySources: map[string][]string{}}}
	r.Trace.Layers = []Layer{{Name: "built-in default"}}
	for range r.Settings.Shell {
		r.Trace.EntrySources["shell"] = append(r.Trace.EntrySources["shell"], "built-in default")
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return r, err
		}
		if seen[source.Path] {
			return r, fmt.Errorf("configuration directory selected more than once: %s", source.Path)
		}
		seen[source.Path] = true
		file := filepath.Join(source.Path, "config.json")
		var layer config.Layer
		if preview := proposed.layer(source); preview != nil {
			layer = *preview
			if layer.Raw != nil {
				layer, err = config.ResolveLayer(layer.Raw, file, host)
			}
		} else {
			layer, err = config.ReadLayer(file, host)
		}
		if err != nil {
			return r, err
		}
		r.Layers = append(r.Layers, Layer{Name: source.Label, Path: source.Path, Config: layer})
	}
	selectedHarness := ""
	for _, layer := range r.Layers {
		if layer.Config.Harness != nil {
			selectedHarness = *layer.Config.Harness
		}
	}
	for _, layer := range r.Layers {
		// Configured arguments belong only to the harness named by their own
		// source; changing the final selection must not leak another tool's args.
		if layer.Config.Harness == nil || *layer.Config.Harness != selectedHarness {
			layer.Config.HarnessArgs = nil
		}
		r.Settings.Apply(layer.Config)
		r.Trace.Layers = append(r.Trace.Layers, layer)
		r.Trace.contributions(layer.Name, layer.Config)
		for _, name := range ArtifactNames {
			path, err := fsutil.Path(layer.Path, name)
			if err != nil {
				return r, err
			}
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return r, err
			}
			if !info.Mode().IsRegular() {
				return r, fmt.Errorf("artifact must be a regular file: %s", path)
			}
			r.Trace.Artifacts[name] = append(r.Trace.Artifacts[name], path)
		}
	}
	if err = r.Settings.ValidateFields(); err != nil {
		return r, err
	}
	return r, nil
}

func (t *Trace) contributions(name string, l config.Layer) {
	for key, set := range map[string]bool{"shell": l.Shell != nil, "harness": l.Harness != nil, "network": l.Network != nil, "base_image": l.BaseImage != nil} {
		if set {
			t.Sources[key] = []string{name}
		}
	}
	if l.Shell != nil {
		t.EntrySources["shell"] = nil
		for range *l.Shell {
			t.EntrySources["shell"] = append(t.EntrySources["shell"], name)
		}
	}
	for key, n := range map[string]int{"harness_args": len(l.HarnessArgs), "docker_args": len(l.DockerArgs), "mounts": len(l.Mounts), "env": len(l.Env), "ports": len(l.Ports), "vscode.extensions": len(l.VSCode.Extensions)} {
		t.appendContribution(key, name, n)
	}
}

func (t *Trace) appendContribution(key, source string, count int) {
	if count == 0 {
		return
	}
	t.Sources[key] = append(t.Sources[key], source)
	for i := 0; i < count; i++ {
		t.EntrySources[key] = append(t.EntrySources[key], source)
	}
}

type File struct {
	Data   []byte
	Mode   os.FileMode
	Source string
	Layer  string
}

func (r Resolved) Tree(h harness.Effective) (map[string]File, []string, error) {
	files := map[string]File{}
	warnings := append([]string(nil), h.Warnings...)
	for path, file := range h.Defaults {
		files[path] = File{Data: append([]byte(nil), file.Data...), Mode: file.Mode, Source: h.Origin, Layer: "harness defaults"}
	}
	for _, layer := range r.Layers {
		tree, err := harness.ReadTree(filepath.Join(layer.Path, h.Definition.Name))
		warnings = append(warnings, tree.Warnings...)
		if err != nil {
			return nil, warnings, err
		}
		for path, file := range tree.Files {
			files[path] = File{Data: file.Data, Mode: file.Mode, Source: filepath.Join(layer.Path, h.Definition.Name, path), Layer: layer.Name}
		}
	}
	return files, warnings, nil
}
