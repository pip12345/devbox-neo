// Package store owns durable sessions and their external mutation locks.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
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
	OnExit   string   `json:"on_exit"`
}
type DefinitionInput struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Hash   string `json:"hash"`
}
type Record struct {
	Version         int                      `json:"version"`
	ID              string                   `json:"id"`
	Identity        environment.Identity     `json:"identity"`
	Created         time.Time                `json:"created_at"`
	Activity        time.Time                `json:"last_activity"`
	Action          string                   `json:"last_action"`
	Applied         environment.Fingerprints `json:"fingerprints"`
	ImageTag        string                   `json:"image_tag"`
	ImageID         string                   `json:"image_id"`
	Creation        docker.CreatePlan        `json:"creation"`
	Definition      DefinitionInput          `json:"definition_input"`
	Stores          []harness.Store          `json:"stores"`
	Auth            []harness.Auth           `json:"auth"`
	Config          harness.Config           `json:"config"`
	Merge           []harness.Merge          `json:"config_merge"`
	Prepare         [][]string               `json:"prepare"`
	Launch          Launch                   `json:"launch"`
	Setup           environment.Hook         `json:"setup"`
	SetupContainer  string                   `json:"setup_container"`
	Ownership       int                      `json:"ownership_version"`
	ManifestVersion int                      `json:"manifest_version"`
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r Record) Validate(name string) error {
	if r.Version != 1 || r.Ownership != 1 || r.ManifestVersion != 1 {
		return fmt.Errorf("unsupported session record version")
	}
	if !idPattern.MatchString(r.ID) || r.Identity.Name != name || !validName(name) || !filepath.IsAbs(r.Identity.Workspace) || r.Identity.Slot == "" {
		return fmt.Errorf("invalid session identity")
	}
	if r.Identity.Name != environment.ContainerName(r.Identity.Workspace, r.Identity.Slot) || filepath.Clean(r.Identity.Workspace) != r.Identity.Workspace {
		return fmt.Errorf("recorded workspace/slot does not match its container name")
	}
	if (r.Identity.Slot == "project" && r.Identity.Profile != "") || (r.Identity.Slot != "project" && (!config.Name.MatchString(r.Identity.Profile) || r.Identity.Slot != "profile:"+r.Identity.Profile)) {
		return fmt.Errorf("invalid recorded slot")
	}
	if !strings.HasPrefix(r.ImageID, "sha256:") || !hashPattern.MatchString(strings.TrimPrefix(r.ImageID, "sha256:")) || r.ImageTag != docker.Namespace+"/session:"+r.ID || r.Creation.Name != name || r.Creation.Image != r.ImageID {
		return fmt.Errorf("incomplete recorded creation contract")
	}
	if !hashPattern.MatchString(r.Applied.Image) || !hashPattern.MatchString(r.Applied.Container) || !hashPattern.MatchString(r.Applied.Runtime) || !hashPattern.MatchString(r.Definition.Hash) {
		return fmt.Errorf("invalid recorded fingerprints")
	}
	if r.Launch.Binary == "" || len(r.Launch.Shell) == 0 || (r.Launch.OnExit != "stop" && r.Launch.OnExit != "running") || !config.Name.MatchString(r.Definition.Name) {
		return fmt.Errorf("invalid recorded launch contract")
	}
	if r.Created.IsZero() || r.Activity.IsZero() || !hashPattern.MatchString(r.SetupContainer) {
		return fmt.Errorf("incomplete creation commit")
	}
	if r.Definition.Origin != "builtin" && !filepath.IsAbs(r.Definition.Origin) {
		return fmt.Errorf("invalid recorded definition source")
	}
	if (r.Setup.Path != "" && (!filepath.IsAbs(r.Setup.Path) || !hashPattern.MatchString(r.Setup.Hash))) || (r.Setup.Path == "" && r.Setup.Hash != "") {
		return fmt.Errorf("invalid recorded setup input")
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
	if len(r.Creation.Mounts) != len(targets) {
		return fmt.Errorf("incomplete recorded mounts")
	}
	for _, m := range r.Creation.Mounts {
		seen, known := targets[m.Target]
		if !known || seen || !filepath.IsAbs(m.Source) || (m.Target == "/workspace" && m.Source != r.Identity.Workspace) {
			return fmt.Errorf("invalid recorded mount")
		}
		targets[m.Target] = true
	}
	if err := (config.Settings{OnExit: r.Launch.OnExit, Shell: r.Launch.Shell, Harness: r.Definition.Name, Network: r.Creation.Network}).Validate(); err != nil {
		return fmt.Errorf("invalid recorded settings: %w", err)
	}
	return nil
}
func validName(name string) bool {
	return strings.HasPrefix(name, docker.Namespace+"-") && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}
func Open(ctx context.Context, home string) (*Store, error) {
	absolute, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if _, err = fsutil.Dir(absolute, "state/locks/sessions", 0700); err != nil {
		return nil, err
	}
	lock, err := lockFile(ctx, filepath.Join(absolute, "state/locks/installation.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock(lock)
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
	f, err := lockFile(ctx, p)
	if err != nil {
		return nil, err
	}
	return &Locked{ctx: ctx, store: s, Name: name, file: f}, nil
}
func (l *Locked) Close() error {
	if l.file == nil {
		return nil
	}
	err := unlock(l.file)
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
	if err := l.check(); err != nil {
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
	lock, err := lockFile(ctx, p)
	if err != nil {
		return record, err
	}
	defer unlock(lock)
	path, err := fsutil.Path(s.Home, filepath.Join("sessions", name, "session.json"))
	if err != nil {
		return record, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err = config.Decode(b, &record); err != nil {
		return record, fmt.Errorf("corrupt session record: %w", err)
	}
	return record, record.Validate(name)
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
	lock, err := lockFile(l.ctx, p)
	if err != nil {
		return err
	}
	defer unlock(lock)
	if _, err = l.Dir("."); err != nil {
		return err
	}
	path, err := l.Path("session.json")
	if err != nil {
		return err
	}
	return fsutil.JSON(path, record)
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

func lockFile(ctx context.Context, path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func unlock(f *os.File) error {
	return errors.Join(syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close())
}
