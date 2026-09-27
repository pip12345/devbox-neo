package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/harness"
	"devbox/internal/sshshare"
)

type Launch struct {
	Binary   string   `json:"binary"`
	Args     []string `json:"args"`
	Continue []string `json:"continue_args"`
	Shell    []string `json:"shell"`
}
type DefinitionInput struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Hash   string `json:"hash"`
}
type Settings struct {
	environment.Binding
	Sources     []config.Reference `json:"sources"`
	ManualStart bool               `json:"manual_start"`
}

type Record struct {
	Version  int          `json:"version"`
	ID       string       `json:"id"`
	Settings Settings     `json:"settings"`
	Applied  AppliedState `json:"applied"`
	Created  time.Time    `json:"created_at"`
	Activity time.Time    `json:"last_activity"`
	Action   string       `json:"last_action"`
	// Directory is discovered by the store, never inferred from settings or
	// persisted as another source of identity.
	Directory string `json:"-"`
}

type AppliedState struct {
	Fingerprints    environment.Fingerprints `json:"fingerprints"`
	Inputs          environment.Inputs       `json:"inputs"`
	ImageTag        string                   `json:"image_tag"`
	ImageID         string                   `json:"image_id"`
	Creation        docker.CreatePlan        `json:"creation"`
	EnvSources      []config.EnvSource       `json:"env_sources,omitempty"`
	Definition      DefinitionInput          `json:"definition_input"`
	Stores          []harness.Store          `json:"stores"`
	Auth            []harness.Auth           `json:"auth"`
	Config          harness.Config           `json:"config"`
	Merge           []harness.Merge          `json:"config_merge"`
	Prepare         [][]string               `json:"prepare"`
	Launch          Launch                   `json:"launch"`
	Setup           []environment.Hook       `json:"setup"`
	SetupContainer  string                   `json:"setup_container"`
	Ownership       int                      `json:"ownership_version"`
	ManifestVersion int                      `json:"manifest_version"`
}

const RecordVersion = 6

// Runtime synchronization must advance its explanation baseline together with
// its fingerprint. Image/container inputs remain committed until recreation.
func (r *Record) ApplyRuntime(inputs environment.RuntimeInputs) {
	r.Applied.Inputs.Runtime = inputs
	r.Applied.Fingerprints.Runtime = inputs.Fingerprint()
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s Settings) Validate() error {
	if err := s.Binding.Validate(); err != nil {
		return err
	}
	for _, source := range s.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r Record) Validate(directory string) error {
	if r.Version != RecordVersion {
		return fmt.Errorf("unsupported session record version; reset development state explicitly")
	}
	if !idPattern.MatchString(r.ID) || !validName(directory) {
		return fmt.Errorf("invalid session identity or directory")
	}
	if r.Created.IsZero() || r.Activity.IsZero() {
		return fmt.Errorf("incomplete session metadata")
	}
	if err := r.Settings.Validate(); err != nil {
		return err
	}
	return r.Applied.Validate(r.ID)
}

func (a AppliedState) Validate(sessionID string) error {
	if a.Ownership != 1 || a.ManifestVersion != 1 {
		return fmt.Errorf("unsupported applied runtime contract")
	}
	if !strings.HasPrefix(a.ImageID, "sha256:") || !hashPattern.MatchString(strings.TrimPrefix(a.ImageID, "sha256:")) || a.ImageTag != docker.Namespace+"/session:"+sessionID || !environment.ValidResourceName(a.Creation.Name) || a.Creation.Image != a.ImageID {
		return fmt.Errorf("incomplete recorded creation contract")
	}
	if !hashPattern.MatchString(a.Fingerprints.Image) || !hashPattern.MatchString(a.Fingerprints.Container) || !hashPattern.MatchString(a.Fingerprints.Runtime) || !hashPattern.MatchString(a.Definition.Hash) {
		return fmt.Errorf("invalid recorded fingerprints")
	}
	if !filepath.IsAbs(a.Inputs.Container.Workspace) || filepath.Clean(a.Inputs.Container.Workspace) != a.Inputs.Container.Workspace {
		return fmt.Errorf("invalid applied workspace")
	}
	if err := a.Inputs.Validate(); err != nil {
		return err
	}
	if a.Inputs.Image.Harness != a.Definition.Name || a.Inputs.Image.Definition.Hash != a.Definition.Hash || a.Inputs.FingerprintsFor(a.ImageID) != a.Fingerprints {
		return fmt.Errorf("recorded inputs do not match the committed fingerprints or identity")
	}
	if a.Launch.Binary == "" || len(a.Launch.Shell) == 0 || !config.Name.MatchString(a.Definition.Name) {
		return fmt.Errorf("invalid recorded launch contract")
	}
	if !hashPattern.MatchString(a.SetupContainer) {
		return fmt.Errorf("incomplete creation commit")
	}
	if a.Definition.Origin != "builtin" && !filepath.IsAbs(a.Definition.Origin) {
		return fmt.Errorf("invalid recorded definition source")
	}
	if len(a.Setup) != len(a.Inputs.Container.Setup) {
		return fmt.Errorf("recorded setup chain differs from applied inputs")
	}
	for i, hook := range a.Setup {
		input := a.Inputs.Container.Setup[i]
		if !filepath.IsAbs(hook.Path) || !hashPattern.MatchString(hook.Hash) || hook.Path != input.Source || hook.Hash != input.Hash || input.Directory || input.Mode != 0 {
			return fmt.Errorf("invalid recorded setup input")
		}
	}
	for _, source := range a.EnvSources {
		if !hashPattern.MatchString(source.RawHash) || !hashPattern.MatchString(source.ValueHash) {
			return fmt.Errorf("invalid recorded environment fingerprint")
		}
		switch source.Kind {
		case "file":
			if !filepath.IsAbs(source.Path) || source.Index < 0 || source.Field != "env" {
				return fmt.Errorf("invalid recorded environment source")
			}
		default:
			return fmt.Errorf("unknown recorded environment source kind")
		}
	}
	d := harness.Definition{Version: 1, Name: a.Definition.Name, Binary: a.Launch.Binary, Stores: a.Stores, Config: a.Config, Merge: a.Merge, Auth: a.Auth, Prepare: a.Prepare}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("invalid recorded harness contract: %w", err)
	}
	targets := map[string]bool{"/workspace": false}
	for _, s := range a.Stores {
		targets[s.Target] = false
	}
	for _, a := range a.Auth {
		targets[a.Target] = false
	}
	protected := []string{"/devbox"}
	for target := range targets {
		protected = append(protected, target)
	}
	extra := []docker.Mount{}
	sshMounted := false
	for _, m := range a.Creation.Mounts {
		if err := docker.ValidateStoredMount(m); err != nil {
			return err
		}
		if m.Target == sshshare.Mount {
			if sshMounted || m.Kind == "volume" || m.File || m.ReadOnly {
				return fmt.Errorf("invalid recorded SSH mount")
			}
			sshMounted = true
			continue
		}
		seen, known := targets[m.Target]
		if !known {
			extra = append(extra, m)
			continue
		}
		if seen || m.Kind == "volume" || (m.Target == "/workspace" && (m.Source != a.Inputs.Container.Workspace || m.ReadOnly)) {
			return fmt.Errorf("invalid recorded managed mount")
		}
		targets[m.Target] = true
	}
	for _, seen := range targets {
		if !seen {
			return fmt.Errorf("incomplete recorded mounts")
		}
	}
	if err := docker.ValidateExtraTargets(extra, protected); err != nil {
		return err
	}
	for _, port := range a.Creation.Ports {
		if err := docker.ValidatePort(port); err != nil {
			return err
		}
	}
	if a.Creation.Network == "host" && len(a.Creation.Ports) > 0 {
		return fmt.Errorf("host networking cannot publish ports")
	}
	if a.Creation.Metadata != "" && !json.Valid([]byte(a.Creation.Metadata)) {
		return fmt.Errorf("invalid IDE metadata")
	}
	if err := (config.Settings{Shell: a.Launch.Shell, Harness: a.Definition.Name, Network: a.Creation.Network}).Validate(); err != nil {
		return fmt.Errorf("invalid recorded settings: %w", err)
	}
	return nil
}
