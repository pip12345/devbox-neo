// Package artifact is the only owner of profile/project participation and precedence.
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
	Excluded  []string            `json:"excluded"`
	Artifacts map[string][]string `json:"artifacts"`
	Sources   map[string][]string `json:"sources"`
	// EntrySources follows the resolved list order, including duplicate values.
	EntrySources map[string][]string `json:"entry_sources,omitempty"`
}
type Resolved struct {
	Global    config.Global
	Settings  config.Settings
	Layers    []Layer
	Trace     Trace
	Profile   string
	Project   bool
	Selection Participation
}

func Resolve(home, workspace, explicit string, override config.Layer) (Resolved, error) {
	return ResolveWithHost(home, workspace, explicit, override, config.Snapshot())
}

func ResolveWithHost(home, workspace, explicit string, override config.Layer, host config.Host) (Resolved, error) {
	return resolve(home, workspace, Selection{Profile: explicit}, override, nil, host)
}

// PreviewProject uses the normal participation rules for a proposed project
// edit, before writing it. Initialization must not implement its own inheritance.
func PreviewProject(home, workspace string, project config.Layer, host config.Host) (Resolved, error) {
	return Preview(home, workspace, "", config.Layer{}, &project, host)
}

// Preview resolves a proposed project edit through the same participation
// rules, without publishing it.
func Preview(home, workspace, explicit string, override config.Layer, project *config.Layer, host config.Host) (Resolved, error) {
	return resolve(home, workspace, Selection{Profile: explicit}, override, project, host)
}

func PreviewSelection(home, workspace string, selection Selection, override config.Layer, project *config.Layer, host config.Host) (Resolved, error) {
	return resolve(home, workspace, selection, override, project, host)
}

func resolve(home, workspace string, selection Selection, override config.Layer, proposed *config.Layer, host config.Host) (r Resolved, err error) {
	defer func() {
		var actionable *commanderror.Error
		if err != nil && !errors.As(err, &actionable) {
			err = commanderror.New("invalid_configuration", "Invalid configuration: "+err.Error(), workspace, err)
		}
	}()
	r = Resolved{Settings: config.Defaults(), Trace: Trace{Artifacts: map[string][]string{}, Sources: map[string][]string{}, EntrySources: map[string][]string{}}}
	for range r.Settings.Shell {
		r.Trace.EntrySources["shell"] = append(r.Trace.EntrySources["shell"], "built-in default")
	}
	g, err := config.ReadGlobal(filepath.Join(home, "config.json"), host)
	if err != nil {
		return r, err
	}
	r.Global = g
	r.Settings.Env = append(r.Settings.Env, g.GlobalEnv...)
	r.Settings.EnvInputs = append(r.Settings.EnvInputs, g.EnvInputs...)
	r.Trace.appendContribution("env", "global", len(g.GlobalEnv))
	participation, err := Select(home, workspace, selection, proposed, host)
	if err != nil {
		return r, err
	}
	r.Selection = participation
	r.Profile, r.Project = participation.Profile, participation.Project
	if !r.Project {
		r.Trace.Excluded = append(r.Trace.Excluded, "project")
	}
	if r.Profile == "" {
		r.Trace.Excluded = append(r.Trace.Excluded, "profile")
	}
	for i, source := range participation.Sources {
		file := filepath.Join(source.Path, "config.json")
		var layer config.Layer
		if preview := participation.Preview.layer(source); preview != nil {
			layer = *preview
			if layer.Raw != nil {
				layer, err = config.ResolveLayer(layer.Raw, file, host)
			}
		} else {
			layer, err = config.ReadLayer(file, host)
		}
		if err != nil {
			var pathErr *os.PathError
			if participation.Profile != "" && errors.As(err, &pathErr) && os.IsNotExist(pathErr) && pathErr.Path == filepath.Join(home, "profiles", participation.Profile, "config.json") {
				return r, commanderror.New("profile_missing", fmt.Sprintf("Profile %q does not exist.", participation.Profile), filepath.Dir(pathErr.Path), err,
					commanderror.Next("Create profile", "profile", "create", participation.Profile),
					commanderror.Next("Then select a harness", "profile", "init", participation.Profile, "--harness", "<name>"))
			}
			return r, err
		}
		// Selection and full resolution must describe the same source revision.
		// Otherwise a concurrent cutoff edit could create an unreachable session.
		header := participation.Headers[i]
		if header.Checked && (layer.Inherit == nil || *layer.Inherit) != header.Inherit {
			return r, fmt.Errorf("configuration identity metadata changed during resolution: %s; retry", file)
		}
		r.Layers = append(r.Layers, Layer{Name: source.Label, Path: source.Path, Config: layer})
	}
	r.Trace.Layers = append(r.Trace.Layers, Layer{Name: "built-in default"}, Layer{Name: "global", Path: filepath.Join(home, "config.json")})
	selectedHarness := g.DefaultHarness
	for _, l := range r.Layers {
		if l.Config.Harness != nil {
			selectedHarness = *l.Config.Harness
		}
	}
	if override.Harness != nil {
		selectedHarness = *override.Harness
	}
	if selectedHarness == "" {
		selectedHarness = g.DefaultHarness
	}
	for _, l := range r.Layers {
		// Arguments belong to the harness named by their own source layer.
		if l.Config.Harness == nil || *l.Config.Harness != selectedHarness {
			l.Config.HarnessArgs = nil
		}
		r.Settings.Apply(l.Config)
		r.Trace.Layers = append(r.Trace.Layers, l)
		r.Trace.contributions(l.Name, l.Config)
		for _, name := range ArtifactNames {
			p, err := fsutil.Path(l.Path, name)
			if err != nil {
				return r, err
			}
			info, err := os.Lstat(p)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return r, err
			}
			if !info.Mode().IsRegular() {
				return r, fmt.Errorf("artifact must be a regular file: %s", p)
			}
			r.Trace.Artifacts[name] = append(r.Trace.Artifacts[name], p)
		}
	}
	for _, value := range override.Env {
		if err := config.ValidateEnvAssignment(value); err != nil {
			return r, err
		}
		override.EnvInputs = append(override.EnvInputs, config.EnvInput{Value: value, Source: config.EnvSource{Kind: "invocation"}})
	}
	r.Settings.Apply(override)
	r.Trace.contributions("CLI", override)
	if r.Settings.Harness == "" {
		r.Settings.Harness = g.DefaultHarness
		if g.DefaultHarness != "" {
			r.Trace.Sources["harness"] = []string{"global"}
		} else {
			delete(r.Trace.Sources, "harness")
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
	for p, b := range h.Defaults {
		files[p] = File{Data: append([]byte(nil), b.Data...), Mode: b.Mode, Source: h.Origin, Layer: "harness defaults"}
	}
	for _, l := range r.Layers {
		tree, err := harness.ReadTree(filepath.Join(l.Path, h.Definition.Name))
		warnings = append(warnings, tree.Warnings...)
		if err != nil {
			return nil, warnings, err
		}
		for p, b := range tree.Files {
			files[p] = File{Data: b.Data, Mode: b.Mode, Source: filepath.Join(l.Path, h.Definition.Name, p), Layer: l.Name}
		}
	}
	return files, warnings, nil
}
