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
	"path/filepath"
	"regexp"
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
const ContainerPrefix = "devbox-"

type Identity struct {
	Workspace  string `json:"workspace"`
	Slot       string `json:"slot"`
	Name       string `json:"name"`
	Profile    string `json:"profile,omitempty"`
	Project    bool   `json:"project"`
	ProjectDir string `json:"project_dir,omitempty"`
}

func Identify(workspace, profile string, project bool) (Identity, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return Identity{}, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Identity{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Identity{}, err
	}
	if !info.IsDir() {
		return Identity{}, fmt.Errorf("workspace must be a directory")
	}
	slot := Slot(profile, project)
	if _, _, err := ParseSlot(slot); err != nil {
		return Identity{}, err
	}
	return Identity{Workspace: canonical, Slot: slot, Profile: profile, Project: project, Name: ContainerName(canonical, slot)}, nil
}

var unsafeFolderCharacters = regexp.MustCompile(`[^a-z0-9_.-]+`)

func ContainerName(workspace, slot string) string {
	// The folder is a readable hint, not identity: truncation and sanitization
	// must not change which full workspace path and slot feed the hash.
	folder := unsafeFolderCharacters.ReplaceAllString(strings.ToLower(filepath.Base(workspace)), "-")
	folder = strings.Trim(folder, "-_.")
	folder = strings.TrimRight(folder[:min(len(folder), 32)], "-_.")
	if folder == "" {
		folder = "workspace"
	}
	sum := sha256.Sum256([]byte(workspace + "\x00" + slot))
	return ContainerPrefix + folder + "-" + hex.EncodeToString(sum[:6]) + "." + slot
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
	Identity     Identity
	Settings     config.Settings
	Harness      harness.Effective
	Trace        artifact.Trace
	Files        map[string]artifact.File
	Warnings     []string
	Build        ImageBuildPlan
	Setup        []Hook
	BeforeOpen   []Hook
	Sources      []config.Source
	Fingerprints Fingerprints
	Inputs       Inputs
	EnvSources   []config.EnvSource
	ExtraMounts  []docker.Mount
	Metadata     string
	Host         config.Host `json:"-"`
}
type Request struct {
	Salt          string
	Home          string
	Workspace     string
	Profile       string
	Overrides     config.Layer
	IgnoreProject bool
	ProjectDir    string
	Sources       []config.Source
	Recorded      *Identity
	UID           int
	GID           int
	Host          config.Host `json:"-"`
}

func Resolve(q Request) (Spec, error) { return resolve(q, nil) }

// Preview validates proposed project configuration without publishing it.
// Its specification is for comparison only; execution resolves final paths.
func Preview(q Request, project *config.Layer) (Spec, error) { return resolve(q, project) }

func resolve(q Request, project *config.Layer) (Spec, error) {
	var spec Spec
	workspace, err := Identify(q.Workspace, "", true)
	if err != nil {
		return spec, err
	}
	q.Workspace = workspace.Workspace
	if q.Host == nil {
		q.Host = config.Snapshot()
	} else {
		q.Host = maps.Clone(q.Host)
	}
	selection := artifact.Selection{Profile: q.Profile, IgnoreProject: q.IgnoreProject, ProjectDir: q.ProjectDir, Sources: q.Sources}
	if q.Recorded != nil {
		selection.Recorded = &artifact.Participation{Profile: q.Recorded.Profile, Project: q.Recorded.Project, ProjectDir: q.Recorded.ProjectDir, Sources: q.Sources}
	}
	r, err := artifact.PreviewSelection(q.Home, q.Workspace, selection, q.Overrides, project, q.Host)
	if err != nil {
		return spec, err
	}
	identity := Identity{Workspace: q.Workspace, Profile: r.Profile, Project: r.Project, ProjectDir: r.Selection.ProjectDir, Slot: Slot(r.Profile, r.Project)}
	identity.Name = ContainerName(identity.Workspace, identity.Slot)
	if err = identity.ValidateSlot(); err != nil {
		return spec, err
	}
	if q.Recorded != nil && identity != *q.Recorded {
		return spec, fmt.Errorf("requested combination does not match its recorded identity")
	}
	if err = r.Settings.Validate(); err != nil {
		var actionable *commanderror.Error
		if errors.As(err, &actionable) && actionable.Code == "harness_required" {
			step := commanderror.Next("Select a harness", "project", "init", workspace.Workspace, "--harness", "<name>")
			if identity.Profile != "" {
				step = commanderror.Next("Select a harness", "profile", "init", identity.Profile, "--harness", "<name>")
			}
			return spec, commanderror.New(actionable.Code, actionable.Message, identity.Name, err, step)
		}
		return spec, commanderror.New("invalid_configuration", "Invalid configuration: "+err.Error(), workspace.Workspace, err)
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
	spec = Spec{Identity: identity, Settings: r.Settings, Harness: h, Trace: r.Trace, Files: files, Warnings: warnings, Host: q.Host, Sources: r.Selection.Sources}
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
	spec.Build, err = PlanImage(r.Trace.Artifacts["Dockerfile"], r.Settings.BaseImage, h.Definition, q.UID, q.GID)
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
