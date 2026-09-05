// Package artifact is the only owner of profile/project participation and precedence.
package artifact

import (
	"fmt"
	"os"
	"path/filepath"

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
	Layers   []Layer             `json:"layers"`
	Excluded []string            `json:"excluded"`
	Winners  map[string]string   `json:"artifact_winners"`
	Sources  map[string][]string `json:"sources"`
}
type Resolved struct {
	Settings config.Settings
	Layers   []Layer
	Trace    Trace
	Profile  string
	Project  bool
}

func Resolve(home, workspace, explicit string, override config.Layer) (Resolved, error) {
	return resolve(home, workspace, explicit, override, nil)
}

// PreviewProject uses the normal participation rules for a proposed project
// edit, before writing it. Initialization must not implement its own inheritance.
func PreviewProject(home, workspace string, project config.Layer) (Resolved, error) {
	return resolve(home, workspace, "", config.Layer{}, &project)
}

func resolve(home, workspace, explicit string, override config.Layer, proposed *config.Layer) (Resolved, error) {
	r := Resolved{Settings: config.Defaults(), Trace: Trace{Winners: map[string]string{}, Sources: map[string][]string{}}}
	g, err := config.ReadGlobal(filepath.Join(home, "config.json"))
	if err != nil {
		return r, err
	}
	r.Settings.Env = append(r.Settings.Env, g.GlobalEnv...)
	profile := explicit
	if profile == "" {
		profile = g.DefaultProfile
	}
	projectPath := filepath.Join(workspace, ".devbox", "config.json")
	var project *config.Layer
	if explicit != "" || g.IgnoreProject {
		r.Trace.Excluded = append(r.Trace.Excluded, "project")
	} else {
		var l config.Layer
		var err error
		if proposed != nil {
			l = *proposed
		} else {
			l, err = config.ReadLayer(projectPath, true)
		}
		if err == nil {
			project = &l
			r.Project = true
			if l.InheritProfile != nil && !*l.InheritProfile {
				profile = ""
				r.Trace.Excluded = append(r.Trace.Excluded, "profile")
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return r, err
		}
	}
	if profile != "" {
		if !config.Name.MatchString(profile) {
			return r, fmt.Errorf("invalid profile name")
		}
		root, err := fsutil.Path(home, filepath.Join("profiles", profile))
		if err != nil {
			return r, err
		}
		l, err := config.ReadLayer(filepath.Join(root, "config.json"), false)
		if os.IsNotExist(err) {
			return r, fmt.Errorf("profile %q does not exist; use devbox-neo profile create %s, then devbox-neo profile init %s --harness <name>", profile, profile, profile)
		}
		if err != nil {
			return r, err
		}
		r.Layers = append(r.Layers, Layer{Name: "profile", Path: root, Config: l})
		r.Profile = profile
	}
	if project != nil {
		r.Layers = append(r.Layers, Layer{Name: "project", Path: filepath.Dir(projectPath), Config: *project})
	}
	if len(r.Layers) == 0 {
		return r, fmt.Errorf("no profile or project configuration applies; use devbox-neo profile create <name> and select it with --profile <name>, or devbox-neo project create <folder>")
	}
	r.Trace.Layers = append(r.Trace.Layers, Layer{Name: "built-in default"}, Layer{Name: "global", Path: filepath.Join(home, "config.json")})
	for _, l := range r.Layers {
		r.Settings.Apply(l.Config)
		r.Trace.Layers = append(r.Trace.Layers, l)
		contributions(r.Trace.Sources, l.Name, l.Config)
		for _, name := range SingletonNames {
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
			r.Trace.Winners[name] = p
		}
	}
	r.Settings.Apply(override)
	contributions(r.Trace.Sources, "CLI", override)
	if r.Settings.Harness == "" {
		r.Settings.Harness = g.DefaultHarness
		r.Trace.Sources["harness"] = []string{"global"}
	}
	if err = r.Settings.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
func contributions(out map[string][]string, name string, l config.Layer) {
	for key, set := range map[string]bool{"on_exit": l.OnExit != nil, "default_shell": l.Shell != nil, "harness": l.Harness != nil, "network": l.Network != nil} {
		if set {
			out[key] = []string{name}
		}
	}
	for key, n := range map[string]int{"harness_args": len(l.HarnessArgs), "docker_args": len(l.DockerArgs), "extra_mounts": len(l.Mounts), "extra_env": len(l.Env), "extra_ports": len(l.Ports), "vscode.extensions": len(l.VSCode.Extensions)} {
		if n > 0 {
			out[key] = append(out[key], name)
		}
	}
}

type File struct {
	Data   []byte
	Mode   os.FileMode
	Source string
	Layer  string
}

func (r Resolved) Tree(h harness.Effective) (map[string]File, error) {
	files := map[string]File{}
	for p, b := range h.Defaults {
		files[p] = File{Data: append([]byte(nil), b.Data...), Mode: b.Mode, Source: h.Origin, Layer: "harness defaults"}
	}
	for _, l := range r.Layers {
		tree, err := harness.ReadTree(filepath.Join(l.Path, h.Definition.Name))
		if err != nil {
			return nil, err
		}
		for p, b := range tree {
			files[p] = File{Data: b.Data, Mode: b.Mode, Source: filepath.Join(l.Path, h.Definition.Name, p), Layer: l.Name}
		}
	}
	return files, nil
}
