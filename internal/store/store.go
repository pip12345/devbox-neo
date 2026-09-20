// Package store owns durable sessions and their external mutation locks.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/sshshare"
)

type Store struct {
	Home         string
	Installation string
}
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
type Record struct {
	Version         int                      `json:"version"`
	ManualStart     bool                     `json:"manual_start"`
	ID              string                   `json:"id"`
	Identity        environment.Identity     `json:"identity"`
	Sources         []config.Source          `json:"sources"`
	Created         time.Time                `json:"created_at"`
	Activity        time.Time                `json:"last_activity"`
	Action          string                   `json:"last_action"`
	Applied         environment.Fingerprints `json:"fingerprints"`
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

const RecordVersion = 4

// Runtime synchronization must advance its explanation baseline together with
// its fingerprint. Image/container inputs remain committed until recreation.
func (r *Record) ApplyRuntime(inputs environment.RuntimeInputs) {
	r.Inputs.Runtime = inputs
	r.Applied.Runtime = inputs.Fingerprint()
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r Record) Validate(name string) error {
	if r.Version != RecordVersion || r.Ownership != 1 || r.ManifestVersion != 1 {
		return fmt.Errorf("unsupported session record version; a clean development session reset is required")
	}
	if !idPattern.MatchString(r.ID) || r.Identity.Name != name || !validName(name) || !filepath.IsAbs(r.Identity.Workspace) || r.Identity.Slot == "" {
		return fmt.Errorf("invalid session identity")
	}
	if r.Identity.Name != environment.ContainerName(r.Identity.Workspace, r.Identity.Slot) || filepath.Clean(r.Identity.Workspace) != r.Identity.Workspace {
		return fmt.Errorf("recorded workspace/slot does not match its container name")
	}
	if err := r.Identity.ValidateSlot(); err != nil {
		return err
	}
	if !strings.HasPrefix(r.ImageID, "sha256:") || !hashPattern.MatchString(strings.TrimPrefix(r.ImageID, "sha256:")) || r.ImageTag != docker.Namespace+"/session:"+r.ID || r.Creation.Name != name || r.Creation.Image != r.ImageID {
		return fmt.Errorf("incomplete recorded creation contract")
	}
	if !hashPattern.MatchString(r.Applied.Image) || !hashPattern.MatchString(r.Applied.Container) || !hashPattern.MatchString(r.Applied.Runtime) || !hashPattern.MatchString(r.Definition.Hash) {
		return fmt.Errorf("invalid recorded fingerprints")
	}
	if err := r.Inputs.Validate(); err != nil {
		return err
	}
	if r.Inputs.Container.Identity != r.Identity || r.Inputs.Image.Harness != r.Definition.Name || r.Inputs.Image.Definition.Hash != r.Definition.Hash || r.Inputs.FingerprintsFor(r.ImageID) != r.Applied {
		return fmt.Errorf("recorded inputs do not match the committed fingerprints or identity")
	}
	if r.Launch.Binary == "" || len(r.Launch.Shell) == 0 || !config.Name.MatchString(r.Definition.Name) {
		return fmt.Errorf("invalid recorded launch contract")
	}
	if r.Created.IsZero() || r.Activity.IsZero() || !hashPattern.MatchString(r.SetupContainer) {
		return fmt.Errorf("incomplete creation commit")
	}
	if r.Definition.Origin != "builtin" && !filepath.IsAbs(r.Definition.Origin) {
		return fmt.Errorf("invalid recorded definition source")
	}
	if len(r.Sources) == 0 {
		return fmt.Errorf("recorded configuration sources are missing")
	}
	for _, source := range r.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
	}
	if len(r.Setup) != len(r.Inputs.Container.Setup) {
		return fmt.Errorf("recorded setup chain differs from applied inputs")
	}
	for i, hook := range r.Setup {
		input := r.Inputs.Container.Setup[i]
		if !filepath.IsAbs(hook.Path) || !hashPattern.MatchString(hook.Hash) || hook.Path != input.Source || hook.Hash != input.Hash || input.Directory || input.Mode != 0 {
			return fmt.Errorf("invalid recorded setup input")
		}
	}
	for _, source := range r.EnvSources {
		if !hashPattern.MatchString(source.RawHash) || !hashPattern.MatchString(source.ValueHash) {
			return fmt.Errorf("invalid recorded environment fingerprint")
		}
		switch source.Kind {
		case "file":
			if !filepath.IsAbs(source.Path) || source.Index < 0 || (source.Field != "global_env" && source.Field != "env") {
				return fmt.Errorf("invalid recorded environment source")
			}
		case "invocation":
			if source.Path != "" || source.Field != "" {
				return fmt.Errorf("invalid invocation environment source")
			}
		default:
			return fmt.Errorf("unknown recorded environment source kind")
		}
	}
	d := harness.Definition{Version: 1, Name: r.Definition.Name, Binary: r.Launch.Binary, Stores: r.Stores, Config: r.Config, Merge: r.Merge, Auth: r.Auth, Prepare: r.Prepare}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("invalid recorded harness contract: %w", err)
	}
	targets := map[string]bool{"/workspace": false}
	for _, s := range r.Stores {
		targets[s.Target] = false
	}
	for _, a := range r.Auth {
		targets[a.Target] = false
	}
	protected := []string{"/devbox"}
	for target := range targets {
		protected = append(protected, target)
	}
	extra := []docker.Mount{}
	sshMounted := false
	for _, m := range r.Creation.Mounts {
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
		if seen || m.Kind == "volume" || (m.Target == "/workspace" && (m.Source != r.Identity.Workspace || m.ReadOnly)) {
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
	for _, port := range r.Creation.Ports {
		if err := docker.ValidatePort(port); err != nil {
			return err
		}
	}
	if r.Creation.Network == "host" && len(r.Creation.Ports) > 0 {
		return fmt.Errorf("host networking cannot publish ports")
	}
	if r.Creation.Metadata != "" && !json.Valid([]byte(r.Creation.Metadata)) {
		return fmt.Errorf("invalid IDE metadata")
	}
	if err := (config.Settings{Shell: r.Launch.Shell, Harness: r.Definition.Name, Network: r.Creation.Network}).Validate(); err != nil {
		return fmt.Errorf("invalid recorded settings: %w", err)
	}
	return nil
}
func validName(name string) bool {
	return strings.HasPrefix(name, environment.ContainerPrefix) && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}
func Open(ctx context.Context, home string) (*Store, error) {
	absolute, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if _, err = fsutil.Dir(absolute, "state/locks/sessions", 0700); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(absolute, "state/locks/installation.lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	p, err := fsutil.Path(absolute, "state/installation-id")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		id, e := fsutil.ID()
		if e != nil {
			return nil, e
		}
		if err = fsutil.Write(p, []byte(id+"\n"), 0600); err != nil {
			return nil, err
		}
		b = []byte(id)
	} else if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(b))
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("corrupt installation identity")
	}
	for _, dir := range []string{"profiles", "harnesses", "auth", "cache/harnesses", "sessions"} {
		if _, err = fsutil.Dir(absolute, dir, 0700); err != nil {
			return nil, err
		}
	}
	configPath, err := fsutil.Path(absolute, "config.json")
	if err != nil {
		return nil, err
	}
	if _, err = os.Lstat(configPath); os.IsNotExist(err) {
		if err = fsutil.JSON(configPath, config.Global{Version: 1, GlobalEnv: []string{}}); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &Store{Home: absolute, Installation: id}, nil
}

type Locked struct {
	ctx   context.Context
	store *Store
	Name  string
	file  *os.File
}

func (s *Store) lockPath(name, kind string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("invalid session name")
	}
	return fsutil.Path(s.Home, filepath.Join("state/locks/sessions", environment.Digest(name)+"."+kind+".lock"))
}
func (s *Store) Lock(ctx context.Context, name string) (*Locked, error) {
	p, err := s.lockPath(name, "operation")
	if err != nil {
		return nil, err
	}
	f, err := fsutil.Lock(ctx, p)
	if err != nil {
		return nil, err
	}
	return &Locked{ctx: ctx, store: s, Name: name, file: f}, nil
}
func (l *Locked) Close() error {
	if l.file == nil {
		return nil
	}
	err := fsutil.Unlock(l.file)
	l.file = nil
	return err
}
func (l *Locked) Held() bool { return l.file != nil }
func (l *Locked) check() error {
	if l.file == nil {
		return fmt.Errorf("session operation lock is not held")
	}
	return nil
}
func (l *Locked) Path(rel string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("session path must remain relative")
	}
	return fsutil.Path(l.store.Home, filepath.Join("sessions", l.Name, rel))
}
func (l *Locked) Dir(rel string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("session path must remain relative")
	}
	return fsutil.Dir(l.store.Home, filepath.Join("sessions", l.Name, rel), 0700)
}
func (l *Locked) Load() (Record, error) {
	if err := l.RequireAvailable(); err != nil {
		return Record{}, err
	}
	return l.store.Read(l.ctx, l.Name)
}
func (s *Store) Read(ctx context.Context, name string) (Record, error) {
	var record Record
	p, err := s.lockPath(name, "record")
	if err != nil {
		return record, err
	}
	lock, err := fsutil.Lock(ctx, p)
	if err != nil {
		return record, err
	}
	defer fsutil.Unlock(lock)
	path, err := fsutil.Path(s.Home, filepath.Join("sessions", name, "session.json"))
	if err != nil {
		return record, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err = config.Decode(b, &record); err != nil {
		return record, commanderror.New("invalid_session_record", fmt.Sprintf("Invalid session state: %v", err), path, err)
	}
	if err = record.Validate(name); err != nil {
		return record, commanderror.New("invalid_session_record", "Invalid session state: "+err.Error(), path, err)
	}
	return record, nil
}
func (l *Locked) Save(record Record) error {
	if err := l.check(); err != nil {
		return err
	}
	if err := record.Validate(l.Name); err != nil {
		return err
	}
	p, err := l.store.lockPath(l.Name, "record")
	if err != nil {
		return err
	}
	lock, err := fsutil.Lock(l.ctx, p)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(lock)
	if _, err = l.Dir("."); err != nil {
		return err
	}
	path, err := l.Path("session.json")
	if err != nil {
		return err
	}
	return fsutil.JSON(path, record)
}

// Delete keeps record readers outside the removal window. The external
// operation lock survives deletion and remains held until the caller releases it.
func (l *Locked) Delete() error { return l.DeleteContext(l.ctx) }

// DeleteContext lets bounded cleanup retain the operation lock after the
// foreground context has been cancelled.
func (l *Locked) DeleteContext(ctx context.Context) error {
	if err := l.check(); err != nil {
		return err
	}
	p, err := l.store.lockPath(l.Name, "record")
	if err != nil {
		return err
	}
	lock, err := fsutil.Lock(ctx, p)
	if err != nil {
		return err
	}
	defer fsutil.Unlock(lock)
	root, err := l.Path(".")
	if err != nil {
		return err
	}
	if err = os.RemoveAll(root); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(root))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Touch reloads under the operation lock so a long attached command cannot
// overwrite creation/runtime settings committed by a more recent invocation.
func (l *Locked) Touch(id, action string) (Record, error) {
	r, err := l.Load()
	if err != nil {
		return Record{}, err
	}
	if r.ID != id {
		return Record{}, fmt.Errorf("session identity changed while attached")
	}
	r.Activity = time.Now().UTC()
	r.Action = action
	return r, l.Save(r)
}
