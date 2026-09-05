// Package environment defines immutable desired inputs and change classification.
package environment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/artifact"
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
	Build        ImageBuildPlan
	Setup        Hook
	Entrypoint   Hook
	Fingerprints Fingerprints
	ReadOnly     bool
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
}

func Resolve(q Request) (Spec, error) {
	var spec Spec
	workspace, err := Identify(q.Workspace, "", true)
	if err != nil {
		return spec, err
	}
	q.Workspace = workspace.Workspace
	r, err := artifact.Resolve(q.Home, q.Workspace, q.Profile, q.Overrides)
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
	h, err := harness.Load(q.Home, r.Settings.Harness)
	if err != nil {
		return spec, err
	}
	if q.Salt == "" {
		return spec, fmt.Errorf("installation fingerprint salt is required")
	}
	h.Hash = Fingerprint(q.Salt, h.Hash)
	files, err := r.Tree(h)
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
				return spec, fmt.Errorf("managed config %s overlaps an auth mount; configure managed auth instead", name)
			}
		}
	}
	// Refuse not-yet-delivered creation fields rather than creating an environment
	// that silently ignores requested behavior. Remove each gate with its tests.
	if len(r.Settings.Env)+len(r.Settings.DockerArgs)+len(r.Settings.Mounts)+len(r.Settings.Ports)+len(r.Settings.VSCode.Extensions) > 0 {
		return spec, fmt.Errorf("env, raw Docker args, extra mounts/ports and IDE metadata await phase 3; no environment was changed")
	}
	if q.UID <= 0 || q.GID <= 0 {
		return spec, fmt.Errorf("run the development CLI as a non-root user with a non-root primary group")
	}
	spec = Spec{Identity: identity, Settings: r.Settings, Harness: h, Trace: r.Trace, Files: files, ReadOnly: q.ReadOnly}
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
		Identity Identity
		Network  string
		ReadOnly bool
		Image    string
		Stores   []harness.Store
		Auth     []harness.Auth
		Env      map[string]string
		Setup    string
	}{identity, r.Settings.Network, q.ReadOnly, spec.Fingerprints.Image, h.Definition.Stores, h.Definition.Auth, h.Definition.Env, spec.Setup.Hash})
	data := map[string]harness.File{}
	for p, f := range files {
		data[p] = harness.File{Data: f.Data, Mode: f.Mode & 0111}
	}
	spec.Fingerprints.Runtime = Digest(struct {
		Files      map[string]harness.File
		Entrypoint string
		Launch     harness.Launch
		Args       []string
		OnExit     string
		Shell      []string
	}{data, spec.Entrypoint.Hash, h.Definition.Launch, r.Settings.HarnessArgs, r.Settings.OnExit, r.Settings.Shell})
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
	keys := make([]string, 0, len(s.Harness.Definition.Env))
	for k := range s.Harness.Definition.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+s.Harness.Definition.Env[k])
	}
	return out
}
