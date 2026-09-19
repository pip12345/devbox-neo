package environment

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/harness"
)

// Inputs is the secret-free snapshot of resolved inputs. Fingerprints and
// diagnostic reasons both come from this snapshot, not independently maintained
// lists of settings. Source paths explain changes but do not cause content drift.
type Inputs struct {
	Image     ImageInputs     `json:"image"`
	Container ContainerInputs `json:"container"`
	Runtime   RuntimeInputs   `json:"runtime"`
}

type FileState struct {
	Hash      string `json:"hash"`
	Mode      uint32 `json:"mode"`
	Directory bool   `json:"directory,omitempty"`
}

type FileInput struct {
	FileState
	Source string `json:"source,omitempty"`
}

type ImageInputs struct {
	Mode       string               `json:"mode"`
	Harness    string               `json:"harness"`
	Definition FileInput            `json:"definition"`
	Dockerfile FileInput            `json:"dockerfile"`
	Ignore     FileInput            `json:"ignore"`
	Context    map[string]FileInput `json:"context"`
	Layer      string               `json:"generated_layer"`
	Arguments  map[string]string    `json:"arguments"`
}

type ContainerInputs struct {
	Identity    Identity          `json:"identity"`
	Network     string            `json:"network"`
	Stores      []harness.Store   `json:"stores"`
	Auth        []harness.Auth    `json:"auth"`
	Env         map[string]string `json:"env_hashes"`
	Setup       FileInput         `json:"setup"`
	Mounts      []docker.Mount    `json:"mounts"`
	Ports       []string          `json:"ports"`
	RawArgs     []string          `json:"docker_args"`
	RawArgsHash string            `json:"docker_args_hash"`
	Metadata    string            `json:"metadata"`
	HostAlias   string            `json:"host_alias"`
}

type RuntimeInputs struct {
	Assets     string               `json:"assets"`
	Files      map[string]FileInput `json:"files"`
	Entrypoint FileInput            `json:"entrypoint"`
	Launch     harness.Launch       `json:"launch"`
	Args       []string             `json:"args"`
	Shell      []string             `json:"shell"`
}

func fileInput(salt, source string, data []byte, mode os.FileMode, directory bool) FileInput {
	return FileInput{FileState{Fingerprint(salt, data), uint32(mode), directory}, source}
}

func hookInput(h Hook) FileInput {
	return FileInput{FileState: FileState{Hash: h.Hash}, Source: h.Path}
}

func (p ImageBuildPlan) inputs(h harness.Effective, salt string) ImageInputs {
	image := ImageInputs{Mode: p.Mode, Harness: h.Definition.Name,
		Definition: FileInput{FileState: FileState{Hash: h.Hash}, Source: h.Origin},
		Layer:      Fingerprint(salt, p.Runtime), Arguments: maps.Clone(p.Arguments)}
	if p.Source != "" {
		image.Dockerfile = fileInput(salt, p.Source, p.Dockerfile, 0, false)
	}
	if p.IgnoreSource != "" {
		image.Ignore = fileInput(salt, p.IgnoreSource, p.Ignore, 0, false)
	}
	if p.Context != nil {
		image.Context = map[string]FileInput{}
		for name, file := range p.Context {
			image.Context[name] = fileInput(salt, filepath.Join(filepath.Dir(p.Source), name), file.Data, file.Mode, file.Directory)
		}
	}
	return image
}

func (s Spec) captureInputs(salt, assetsHash string) Inputs {
	container := ContainerInputs{Identity: s.Identity, Network: s.Settings.Network,
		Stores: slices.Clone(s.Harness.Definition.Stores), Auth: slices.Clone(s.Harness.Definition.Auth),
		Env: map[string]string{}, Setup: hookInput(s.Setup), Mounts: slices.Clone(s.ExtraMounts),
		Ports: slices.Clone(s.Settings.Ports), RawArgs: slices.Clone(s.Settings.DockerArgs),
		RawArgsHash: Fingerprint(salt, s.Settings.DockerArgs), Metadata: s.Metadata, HostAlias: docker.HostAlias}
	for _, value := range s.Env() {
		key, _, _ := strings.Cut(value, "=")
		container.Env[key] = Fingerprint(salt, value)
	}
	// Raw --env overrides are excluded by Spec.Env because Docker applies them
	// last. Include their effective values in the diagnostic env hashes, while
	// keeping order/duplicate changes covered by the complete raw-argument hash.
	for i, arg := range container.RawArgs {
		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			key, _, _ := strings.Cut(value, "=")
			container.Env[key] = Fingerprint(salt, value)
			container.RawArgs[i] = "--env=" + key + "=<redacted>"
		}
	}
	runtime := RuntimeInputs{Assets: assetsHash, Files: map[string]FileInput{}, Entrypoint: hookInput(s.Entrypoint),
		Launch: s.Harness.Definition.Launch, Args: slices.Clone(s.Settings.HarnessArgs), Shell: slices.Clone(s.Settings.Shell)}
	for name, file := range s.Files {
		source := file.Source
		// Default-tree provenance identifies the definition. Diagnostics need
		// the actual user-default file, or the builtin origin plus its file key.
		if file.Layer == "harness defaults" && source != "builtin" {
			source = filepath.Join(filepath.Dir(source), "defaults", name)
		}
		runtime.Files[name] = fileInput(salt, source, file.Data, file.Mode&0111, false)
	}
	return Inputs{s.Build.inputs(s.Harness, salt), container, runtime}
}

func fileStates(files map[string]FileInput) map[string]FileState {
	if files == nil {
		return nil
	}
	states := make(map[string]FileState, len(files))
	for name, file := range files {
		states[name] = file.FileState
	}
	return states
}

func (i ImageInputs) fingerprint() string {
	return Digest(struct {
		Mode, Harness, Definition, Layer string
		Dockerfile, Ignore               FileState
		Context                          map[string]FileState
		Arguments                        map[string]string
	}{i.Mode, i.Harness, i.Definition.Hash, i.Layer, i.Dockerfile.FileState, i.Ignore.FileState, fileStates(i.Context), i.Arguments})
}

func (r RuntimeInputs) Fingerprint() string {
	return Digest(struct {
		Assets     string
		Files      map[string]FileState
		Entrypoint FileState
		Launch     harness.Launch
		Args       []string
		Shell      []string
	}{r.Assets, fileStates(r.Files), r.Entrypoint.FileState, r.Launch, r.Args, r.Shell})
}

func (i Inputs) Fingerprints() Fingerprints {
	image := i.Image.fingerprint()
	container := i.Container
	container.Setup.Source = ""
	return Fingerprints{Image: image, Container: Digest(struct {
		Image  string
		Inputs ContainerInputs
	}{image, container}), Runtime: i.Runtime.Fingerprint()}
}

func (i Inputs) FingerprintsFor(imageID string) Fingerprints {
	return i.Fingerprints().ForImage(imageID)
}

func (f Fingerprints) ForImage(imageID string) Fingerprints {
	f.Container = Digest(struct{ Inputs, ImageID string }{f.Container, imageID})
	return f
}

var inputHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (f FileInput) validate(optional bool) error {
	if optional && f == (FileInput{}) {
		return nil
	}
	if !inputHash.MatchString(f.Hash) || f.Mode&^0777 != 0 || (f.Source != "builtin" && !filepath.IsAbs(f.Source)) {
		return fmt.Errorf("invalid recorded file input")
	}
	return nil
}

func (i Inputs) Validate() error {
	if (i.Image.Mode != "default" && i.Image.Mode != "normal") || !config.Name.MatchString(i.Image.Harness) {
		return fmt.Errorf("invalid recorded image inputs")
	}
	for _, hash := range []string{i.Image.Layer, i.Container.RawArgsHash, i.Runtime.Assets} {
		if !inputHash.MatchString(hash) {
			return fmt.Errorf("invalid recorded input fingerprint")
		}
	}
	if i.Container.Env == nil || i.Runtime.Files == nil || (i.Image.Mode == "normal" && i.Image.Context == nil) {
		return fmt.Errorf("incomplete recorded input snapshot")
	}
	if i.Image.Definition.Mode != 0 || i.Image.Definition.Directory || (i.Image.Mode == "default" && (i.Image.Dockerfile != (FileInput{}) || i.Image.Context != nil || i.Image.Ignore != (FileInput{}) || i.Image.Arguments != nil)) {
		return fmt.Errorf("invalid recorded image input layout")
	}
	if err := i.Image.Definition.validate(false); err != nil {
		return err
	}
	if err := i.Image.Dockerfile.validate(i.Image.Mode == "default"); err != nil {
		return err
	}
	for _, file := range []FileInput{i.Image.Ignore, i.Container.Setup, i.Runtime.Entrypoint} {
		if err := file.validate(true); err != nil {
			return err
		}
	}
	for _, files := range []map[string]FileInput{i.Image.Context, i.Runtime.Files} {
		for name, file := range files {
			if !filepath.IsLocal(name) || filepath.Clean(name) != name || name == "." {
				return fmt.Errorf("invalid recorded input file name")
			}
			if err := file.validate(false); err != nil {
				return err
			}
		}
	}
	for name, hash := range i.Container.Env {
		if !config.EnvName.MatchString(name) || !inputHash.MatchString(hash) {
			return fmt.Errorf("invalid recorded environment input")
		}
	}
	for _, arg := range i.Container.RawArgs {
		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			name, value, _ := strings.Cut(value, "=")
			if !config.EnvName.MatchString(name) || value != "<redacted>" {
				return fmt.Errorf("recorded diagnostic Docker environment must be redacted")
			}
		}
	}
	return (config.Settings{Harness: i.Image.Harness, Network: i.Container.Network, Shell: i.Runtime.Shell}).Validate()
}
