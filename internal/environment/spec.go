// Package environment defines immutable desired inputs and change classification.
package environment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"sort"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/assets"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/filesync"
	"devbox/internal/harness"
)

// ContainerPrefix is a lookup convention, independent of Docker ownership labels.
const ContainerPrefix = "dbx-"

// Binding is the user-selected workspace and folder-local name. Neither field
// identifies storage or authorizes a Docker resource.
type Binding struct {
	Workspace string `json:"workspace"`
	LocalName string `json:"local_name"`
}

// Identity describes a named runtime or transfer endpoint, not session identity.
type Identity struct {
	Binding
	Name string `json:"name"`
}

func ResourceName(_ string, localName, allocation string) string {
	// The allocation suffix prevents reused names from colliding with resources
	// whose readable local-name hint is now stale.
	sum := sha256.Sum256([]byte(allocation))
	return ContainerPrefix + hex.EncodeToString(sum[:6]) + "." + localName
}

type Fingerprints struct {
	Image     string `json:"image"`
	Container string `json:"container"`
	Runtime   string `json:"runtime"`
}
type Hook struct {
	Path string `json:"path,omitempty"`
	Hash string `json:"hash,omitempty"`
	Data []byte `json:"-"`
}
type Spec struct {
	Identity        Identity
	Settings        config.Settings
	Harness         harness.Effective
	Trace           artifact.Trace
	Files           map[string]artifact.File
	Warnings        []string
	Build           ImageBuildPlan
	Setup           []Hook
	BeforeOpen      []Hook
	Sources         []config.Reference
	ResolvedSources []config.Source
	Fingerprints    Fingerprints
	Inputs          Inputs
	EnvSources      []config.EnvSource
	ExtraMounts     []docker.Mount
	Metadata        string
	Host            config.Host `json:"-"`
}
type Request struct {
	Salt      string
	Home      string
	Workspace string
	LocalName string
	Sources   []config.Reference
	SessionID string
	UID       int
	GID       int
	Host      config.Host `json:"-"`
}

func Resolve(q Request) (Spec, error) {
	var spec Spec
	identity, err := Identify(q.Workspace, q.LocalName)
	if err != nil {
		return spec, err
	}
	q.Workspace = identity.Workspace
	if q.Host == nil {
		q.Host = config.Snapshot()
	} else {
		q.Host = maps.Clone(q.Host)
	}
	sources, err := config.ResolveReferences(q.Workspace, q.Sources)
	if err != nil {
		var next []commanderror.Step
		if q.SessionID != "" {
			next = append(next, commanderror.Next("Repair the session's selected configs", "edit", q.SessionID))
		}
		return spec, commanderror.New("configuration_unavailable", "Cannot resolve selected configs: "+err.Error(), q.SessionID, err, next...)
	}
	r, err := artifact.Resolve(sources, q.Host)
	if err != nil {
		if q.SessionID != "" {
			return spec, commanderror.New("invalid_configuration", err.Error(), q.SessionID, err,
				commanderror.Next("Inspect and repair selected configs", "edit", q.SessionID))
		}
		return spec, err
	}
	if err = r.Settings.Validate(); err != nil {
		var actionable *commanderror.Error
		if errors.As(err, &actionable) && actionable.Code == "harness_required" {
			step := commanderror.Next("Select a harness in a config", "config", "edit", sources[0].Path, "--harness", "<name>")
			return spec, commanderror.New(actionable.Code, actionable.Message, q.SessionID, err, step)
		}
		return spec, commanderror.New("invalid_configuration", "Invalid configuration: "+err.Error(), q.Workspace, err)
	}
	h, err := harness.Load(q.Home, r.Settings.Harness)
	if err != nil {
		return spec, err
	}
	if q.Salt == "" {
		return spec, fmt.Errorf("installation fingerprint salt is required")
	}
	h.Hash = Fingerprint(q.Salt, h.Hash)
	files, warnings, err := r.Tree(h)
	if err != nil {
		return spec, err
	}
	if err = filesync.Validate(files, h.Definition.Merge); err != nil {
		return spec, err
	}
	configRoot := ""
	for _, store := range h.Definition.Stores {
		if store.Name == h.Definition.Config.Store {
			configRoot = path.Join(store.Target, h.Definition.Config.Path)
		}
	}
	for name := range files {
		target := path.Join(configRoot, name)
		for _, auth := range h.Definition.Auth {
			if target == auth.Target || strings.HasPrefix(auth.Target, target+"/") || (auth.Kind == "directory" && strings.HasPrefix(target, auth.Target+"/")) {
				return spec, fmt.Errorf("managed config %s overlaps an auth mount.\nConfigure managed auth instead.", name)
			}
		}
	}
	if q.UID <= 0 || q.GID <= 0 {
		return spec, fmt.Errorf("run the development CLI as a non-root user with a non-root primary group")
	}
	spec = Spec{Identity: identity, Settings: r.Settings, Harness: h, Trace: r.Trace, Files: files, Warnings: warnings, Host: q.Host, Sources: append([]config.Reference(nil), q.Sources...), ResolvedSources: sources}
	protected := []string{"/workspace", "/devbox"}
	for _, store := range h.Definition.Stores {
		protected = append(protected, store.Target)
	}
	for _, auth := range h.Definition.Auth {
		protected = append(protected, auth.Target)
	}
	for _, value := range r.Settings.Mounts {
		mount, err := docker.ParseMount(value, q.Workspace, q.Host["HOME"])
		if err != nil {
			return spec, fmt.Errorf("mounts: %w", err)
		}
		spec.ExtraMounts = append(spec.ExtraMounts, mount)
	}
	if err = docker.ValidateExtraTargets(spec.ExtraMounts, protected); err != nil {
		return spec, err
	}
	for _, mount := range spec.ExtraMounts {
		protected = append(protected, mount.Target)
	}
	if err = docker.ValidateRaw(r.Settings.DockerArgs, protected, q.Workspace, q.Host["HOME"]); err != nil {
		return spec, err
	}
	for _, port := range r.Settings.Ports {
		if err = docker.ValidatePort(port); err != nil {
			return spec, fmt.Errorf("ports: %w", err)
		}
	}
	if r.Settings.Network == "host" {
		for _, arg := range r.Settings.DockerArgs {
			if strings.HasPrefix(arg, "--publish=") || arg == "--publish-all" || arg == "--publish-all=true" {
				return spec, fmt.Errorf("host networking cannot publish ports")
			}
		}
	}
	metadata, err := json.Marshal([]any{map[string]any{"remoteUser": "devuser", "containerUser": "devuser", "workspaceFolder": "/workspace", "customizations": map[string]any{"vscode": r.Settings.VSCode}}})
	if err != nil {
		return spec, err
	}
	spec.Metadata = string(metadata)
	winning := map[string]config.EnvInput{}
	for _, input := range r.Settings.EnvInputs {
		name, _, _ := strings.Cut(input.Value, "=")
		winning[name] = input
	}
	for _, arg := range r.Settings.DockerArgs {
		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			key, _, _ := strings.Cut(value, "=")
			delete(winning, key)
		}
	}
	keys := make([]string, 0, len(winning))
	for name := range winning {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		spec.EnvSources = append(spec.EnvSources, winning[name].Seal(q.Salt))
	}
	if err = docker.ValidateEnv(spec.Env()); err != nil {
		return Spec{}, err
	}
	spec.Build, err = PlanImage(r.Trace.Artifacts[artifact.Dockerfile], r.Settings.BaseImage, h, q.UID, q.GID)
	if err != nil {
		return spec, err
	}
	spec.Setup, err = readHooks(r.Trace.Artifacts["setup.sh"])
	if err != nil {
		return spec, err
	}
	spec.BeforeOpen, err = readHooks(r.Trace.Artifacts["before-open.sh"])
	if err != nil {
		return spec, err
	}
	runtimeHash, err := assets.Hash()
	if err != nil {
		return spec, err
	}
	spec.Inputs = spec.captureInputs(q.Salt, runtimeHash)
	spec.Fingerprints = spec.Inputs.Fingerprints()
	return spec, nil
}
func readHooks(paths []string) ([]Hook, error) {
	var hooks []Hook
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, Hook{Path: path, Hash: Digest(b), Data: b})
	}
	return hooks, nil
}
func Fingerprint(salt string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	h := hmac.New(sha256.New, []byte(salt))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func Digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s Spec) FingerprintsFor(imageID string) Fingerprints {
	return s.Fingerprints.ForImage(imageID)
}

type Change string

const (
	NoChange           Change = "NoChange"
	RuntimeSync        Change = "RuntimeSync"
	Recreate           Change = "Recreate"
	RebuildAndRecreate Change = "RebuildAndRecreate"
)

func Compare(applied, desired Fingerprints) Change {
	if applied.Image != desired.Image {
		return RebuildAndRecreate
	}
	if applied.Container != desired.Container {
		return Recreate
	}
	if applied.Runtime != desired.Runtime {
		return RuntimeSync
	}
	return NoChange
}
func (s Spec) Env() []string {
	values := map[string]string{}
	for key, value := range s.Harness.Definition.Env {
		values[key] = value
	}
	for _, input := range s.Settings.EnvInputs {
		key, value, _ := strings.Cut(input.Value, "=")
		values[key] = value
	}
	for _, arg := range s.Settings.DockerArgs {
		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			key, _, _ := strings.Cut(value, "=")
			delete(values, key)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
