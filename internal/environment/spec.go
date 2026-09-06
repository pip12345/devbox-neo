// Package environment defines immutable desired inputs and change classification.
package environment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/assets"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/filesync"
	"devbox/internal/harness"
)

type Identity struct {
	Workspace string `json:"workspace"`
	Slot      string `json:"slot"`
	Name      string `json:"name"`
	Profile   string `json:"profile,omitempty"`
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
	slot := "project"
	selected := ""
	if !project && profile != "" {
		slot = "profile:" + profile
		selected = profile
	}
	return Identity{Workspace: canonical, Slot: slot, Profile: selected, Name: ContainerName(canonical, slot)}, nil
}

func ContainerName(workspace, slot string) string {
	sum := sha256.Sum256([]byte(workspace + "\x00" + slot))
	return docker.Namespace + "-" + hex.EncodeToString(sum[:12]) + "." + strings.ReplaceAll(slot, ":", "-")
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
	Setup        Hook
	Entrypoint   Hook
	Fingerprints Fingerprints
	ReadOnly     bool
	EnvSources   []config.EnvSource
	ExtraMounts  []docker.Mount
	Metadata     string
	Host         config.Host `json:"-"`
}
type Request struct {
	Salt         string
	Home         string
	Workspace    string
	Profile      string
	ExpectedName string
	Overrides    config.Layer
	ReadOnly     bool
	UID          int
	GID          int
	Host         config.Host `json:"-"`
}

func Resolve(q Request) (Spec, error) {
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
	r, err := artifact.ResolveWithHost(q.Home, q.Workspace, q.Profile, q.Overrides, q.Host)
	if err != nil {
		return spec, err
	}
	identity, err := Identify(q.Workspace, r.Profile, r.Project && q.Profile == "")
	if err != nil {
		return spec, err
	}
	if q.ExpectedName != "" {
		identity, err = Identify(q.Workspace, q.Profile, q.Profile == "")
		if err != nil {
			return spec, err
		}
		if identity.Name != q.ExpectedName {
			return spec, fmt.Errorf("requested slot does not match its recorded identity")
		}
	}
	if err = r.Settings.Validate(); err != nil {
		return spec, err
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
	spec = Spec{Identity: identity, Settings: r.Settings, Harness: h, Trace: r.Trace, Files: files, Warnings: warnings, ReadOnly: q.ReadOnly, Host: q.Host}
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
			return spec, fmt.Errorf("extra_mounts: %w", err)
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
			return spec, fmt.Errorf("extra_ports: %w", err)
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
	spec.Build, err = PlanImage(r.Trace.Winners, h.Definition, q.UID, q.GID)
	if err != nil {
		return spec, err
	}
	spec.Setup, err = readHook(r.Trace.Winners["setup.sh"])
	if err != nil {
		return spec, err
	}
	spec.Entrypoint, err = readHook(r.Trace.Winners["entrypoint.sh"])
	if err != nil {
		return spec, err
	}
	spec.Fingerprints.Image = Digest(struct {
		Build      string
		Definition string
	}{spec.Build.InputFingerprint(), h.Hash})
	// Setup runs once per container. A changed setup input is pending creation
	// work, not something a managed-config sync can mark as applied.
	spec.Fingerprints.Container = Fingerprint(q.Salt, struct {
		Identity            Identity
		Network             string
		ReadOnly            bool
		Image               string
		Stores              []harness.Store
		Auth                []harness.Auth
		Env                 []string
		Setup               string
		ExtraMounts         []docker.Mount
		Ports, RawArgs      []string
		Metadata, HostAlias string
	}{identity, r.Settings.Network, q.ReadOnly, spec.Fingerprints.Image, h.Definition.Stores, h.Definition.Auth, spec.Env(), spec.Setup.Hash, spec.ExtraMounts, r.Settings.Ports, r.Settings.DockerArgs, spec.Metadata, docker.HostAlias})
	data := map[string]harness.File{}
	for p, f := range files {
		data[p] = harness.File{Data: f.Data, Mode: f.Mode & 0111}
	}
	runtimeHash, err := assets.Hash()
	if err != nil {
		return spec, err
	}
	spec.Fingerprints.Runtime = Digest(struct {
		Assets     string
		Files      map[string]harness.File
		Entrypoint string
		Launch     harness.Launch
		Args       []string
		OnExit     string
		Shell      []string
	}{runtimeHash, data, spec.Entrypoint.Hash, h.Definition.Launch, r.Settings.HarnessArgs, r.Settings.OnExit, r.Settings.Shell})
	return spec, nil
}
func readHook(path string) (Hook, error) {
	if path == "" {
		return Hook{}, nil
	}
	b, err := os.ReadFile(path)
	return Hook{Path: path, Hash: Digest(b), Data: b}, err
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
func ImageDockerfile(d harness.Definition, uid, gid int) []byte {
	// Harness installation runs before its runtime cache/prefix env is applied:
	// executables stay in the image, not under empty host cache bind mounts.
	base := fmt.Sprintf("FROM debian:bookworm-slim\nUSER root\nRUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl git sudo procps && rm -rf /var/lib/apt/lists/*\nRUN (getent group %d >/dev/null || groupadd -g %d devuser) && (id devuser >/dev/null 2>&1 || useradd -m -s /bin/bash -u %d -g %d devuser) && echo 'devuser ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/devuser && chmod 0440 /etc/sudoers.d/devuser\nUSER devuser\nENV HOME=/home/devuser USER=devuser\nWORKDIR /workspace\n", gid, gid, uid, gid)
	base += fmt.Sprintf("RUN test \"$(id -u devuser)\" = %d && test \"$(id -g devuser)\" = %d\n", uid, gid)
	if d.Install.Shell != "" {
		encoded, _ := json.Marshal([]string{"/bin/bash", "-o", "pipefail", "-c", d.Install.Shell})
		base += "RUN " + string(encoded) + "\n"
	}
	parents, _ := json.Marshal(mountParentCommand(d))
	base += "RUN " + string(parents) + "\n"
	paths := append([]string(nil), d.Install.Path...)
	paths = append(paths, "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin")
	encoded, _ := json.Marshal(strings.Join(paths, ":"))
	base += "ENV PATH=" + string(encoded) + "\n"
	return []byte(base)
}

func (s Spec) FingerprintsFor(imageID string) Fingerprints {
	f := s.Fingerprints
	f.Container = Digest(struct{ Inputs, ImageID string }{f.Container, imageID})
	return f
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
