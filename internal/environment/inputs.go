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

// Inputs is the single secret-free baseline for fingerprints and drift reasons.
// Paths explain inputs; only content, order and effective settings cause drift.
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
type BuildInputs struct {
	Dockerfile FileInput            `json:"dockerfile"`
	Ignore     FileInput            `json:"ignore"`
	Context    map[string]FileInput `json:"context"`
}
type ImageInputs struct {
	BaseImage  string            `json:"base_image"`
	Harness    string            `json:"harness"`
	Definition FileInput         `json:"definition"`
	Stages     []BuildInputs     `json:"stages"`
	Prepared   string            `json:"prepared_layer"`
	Boundary   string            `json:"boundary_layer"`
	Layer      string            `json:"generated_layer"`
	Arguments  map[string]string `json:"arguments"`
}
type ContainerInputs struct {
	Identity    Identity          `json:"identity"`
	Network     string            `json:"network"`
	Stores      []harness.Store   `json:"stores"`
	Auth        []harness.Auth    `json:"auth"`
	Env         map[string]string `json:"env_hashes"`
	Setup       []FileInput       `json:"setup"`
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
	BeforeOpen []FileInput          `json:"before_open"`
	Launch     harness.Launch       `json:"launch"`
	Args       []string             `json:"args"`
	Shell      []string             `json:"shell"`
}

func fileInput(salt, source string, data []byte, mode os.FileMode, directory bool) FileInput {
	return FileInput{FileState{Fingerprint(salt, data), uint32(mode), directory}, source}
}
func hookInputs(hooks []Hook) []FileInput {
	var files []FileInput
	for _, h := range hooks {
		files = append(files, FileInput{FileState: FileState{Hash: h.Hash}, Source: h.Path})
	}
	return files
}
func (p ImageBuildPlan) inputs(h harness.Effective, salt string) ImageInputs {
	image := ImageInputs{BaseImage: p.BaseImage, Harness: h.Definition.Name, Definition: FileInput{FileState: FileState{Hash: h.Hash}, Source: h.Origin}, Prepared: Fingerprint(salt, p.Prepared), Boundary: Fingerprint(salt, p.Boundary), Layer: Fingerprint(salt, p.Runtime), Arguments: maps.Clone(p.Arguments)}
	for _, stage := range p.Stages {
		input := BuildInputs{Dockerfile: fileInput(salt, stage.Source, stage.Dockerfile, 0, false), Context: map[string]FileInput{}}
		if stage.IgnoreSource != "" {
			input.Ignore = fileInput(salt, stage.IgnoreSource, stage.Ignore, 0, false)
		}
		for name, file := range stage.Context {
			input.Context[name] = fileInput(salt, filepath.Join(filepath.Dir(stage.Source), name), file.Data, file.Mode, file.Directory)
		}
		image.Stages = append(image.Stages, input)
	}
	return image
}
func (s Spec) captureInputs(salt, assetsHash string) Inputs {
	container := ContainerInputs{Identity: s.Identity, Network: s.Settings.Network, Stores: slices.Clone(s.Harness.Definition.Stores), Auth: slices.Clone(s.Harness.Definition.Auth), Env: map[string]string{}, Setup: hookInputs(s.Setup), Mounts: slices.Clone(s.ExtraMounts), Ports: slices.Clone(s.Settings.Ports), RawArgs: slices.Clone(s.Settings.DockerArgs), RawArgsHash: Fingerprint(salt, s.Settings.DockerArgs), Metadata: s.Metadata, HostAlias: docker.HostAlias}
	for _, value := range s.Env() {
		key, _, _ := strings.Cut(value, "=")
		container.Env[key] = Fingerprint(salt, value)
	}
	for i, arg := range container.RawArgs {
		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			key, _, _ := strings.Cut(value, "=")
			container.Env[key] = Fingerprint(salt, value)
			container.RawArgs[i] = "--env=" + key + "=<redacted>"
		}
	}
	runtime := RuntimeInputs{Assets: assetsHash, Files: map[string]FileInput{}, BeforeOpen: hookInputs(s.BeforeOpen), Launch: s.Harness.Definition.Launch, Args: slices.Clone(s.Settings.HarnessArgs), Shell: slices.Clone(s.Settings.Shell)}
	for name, file := range s.Files {
		source := file.Source
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
	states := map[string]FileState{}
	for name, file := range files {
		states[name] = file.FileState
	}
	return states
}
func hookStates(files []FileInput) []FileState {
	var result []FileState
	for _, file := range files {
		result = append(result, file.FileState)
	}
	return result
}
func (i ImageInputs) fingerprint() string {
	type stage struct {
		Dockerfile, Ignore FileState
		Context            map[string]FileState
	}
	var stages []stage
	for _, s := range i.Stages {
		stages = append(stages, stage{s.Dockerfile.FileState, s.Ignore.FileState, fileStates(s.Context)})
	}
	return Digest(struct {
		Base, Harness, Definition, Prepared, Boundary, Layer string
		Stages                                               []stage
		Arguments                                            map[string]string
	}{i.BaseImage, i.Harness, i.Definition.Hash, i.Prepared, i.Boundary, i.Layer, stages, i.Arguments})
}
func (r RuntimeInputs) Fingerprint() string {
	return Digest(struct {
		Assets      string
		Files       map[string]FileState
		BeforeOpen  []FileState
		Launch      harness.Launch
		Args, Shell []string
	}{r.Assets, fileStates(r.Files), hookStates(r.BeforeOpen), r.Launch, r.Args, r.Shell})
}
func (i Inputs) Fingerprints() Fingerprints {
	image := i.Image.fingerprint()
	container := i.Container
	container.Setup = slices.Clone(container.Setup)
	for n := range container.Setup {
		container.Setup[n].Source = ""
	}
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
func validateFiles(files map[string]FileInput) error {
	for name, file := range files {
		if !filepath.IsLocal(name) || filepath.Clean(name) != name || name == "." {
			return fmt.Errorf("invalid recorded input file name")
		}
		if err := file.validate(false); err != nil {
			return err
		}
	}
	return nil
}
func (i Inputs) Validate() error {
	if !config.ImageReference.MatchString(i.Image.BaseImage) || !config.Name.MatchString(i.Image.Harness) {
		return fmt.Errorf("invalid recorded image inputs")
	}
	for _, hash := range []string{i.Image.Prepared, i.Image.Boundary, i.Image.Layer, i.Container.RawArgsHash, i.Runtime.Assets} {
		if !inputHash.MatchString(hash) {
			return fmt.Errorf("invalid recorded input fingerprint")
		}
	}
	if i.Container.Env == nil || i.Runtime.Files == nil || i.Image.Arguments == nil {
		return fmt.Errorf("incomplete recorded input snapshot")
	}
	if err := i.Image.Definition.validate(false); err != nil {
		return err
	}
	for _, stage := range i.Image.Stages {
		if stage.Context == nil {
			return fmt.Errorf("incomplete recorded build context")
		}
		if err := stage.Dockerfile.validate(false); err != nil {
			return err
		}
		if err := stage.Ignore.validate(true); err != nil {
			return err
		}
		if err := validateFiles(stage.Context); err != nil {
			return err
		}
	}
	for _, hooks := range [][]FileInput{i.Container.Setup, i.Runtime.BeforeOpen} {
		for _, file := range hooks {
			if err := file.validate(false); err != nil {
				return err
			}
		}
	}
	if err := validateFiles(i.Runtime.Files); err != nil {
		return err
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
	return (config.Settings{Harness: i.Image.Harness, BaseImage: i.Image.BaseImage, Network: i.Container.Network, Shell: i.Runtime.Shell}).Validate()
}
