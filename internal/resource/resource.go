// Package resource owns configuration-owner creation and source edits, not runtime state.
package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

type Service struct {
	Home            string
	IgnoreProject   bool
	SelectedProfile string
}

// For projects, Name retains the entered folder for command hints; Root and
// Workspace remain canonical paths for filesystem operations.
type Owner struct{ Kind, Name, Root, Workspace string }
type Result struct {
	Path     string              `json:"path"`
	Harness  string              `json:"harness,omitempty"`
	Created  []string            `json:"created,omitempty"`
	Skipped  []string            `json:"skipped,omitempty"`
	Warnings []string            `json:"warnings,omitempty"`
	Next     []commanderror.Step `json:"next_steps,omitempty"`
}

func (o Owner) Command(action string) []string { return []string{"devbox-neo", o.Kind, action, o.Name} }
func (o Owner) step(action, reason string) commanderror.Step {
	return commanderror.Step{Command: o.Command(action), Reason: reason}
}

func (s Service) Profile(name string) (Owner, error) {
	if !config.Name.MatchString(name) {
		return Owner{}, fmt.Errorf("invalid profile name")
	}
	root, err := fsutil.Path(s.Home, filepath.Join("profiles", name))
	return Owner{Kind: "profile", Name: name, Root: root}, err
}
func (s Service) Project(folder string) (Owner, error) {
	if strings.HasPrefix(folder, environment.ContainerPrefix) && !strings.ContainsAny(folder, "/\\") {
		r, err := (&store.Store{Home: s.Home}).Read(context.Background(), folder)
		if err != nil {
			return Owner{}, err
		}
		if !r.Identity.Project {
			return Owner{}, fmt.Errorf("session does not use project configuration")
		}
		root, err := fsutil.Path(r.Identity.Workspace, ".devbox")
		return Owner{Kind: "project", Name: folder, Root: root, Workspace: r.Identity.Workspace}, err
	}
	absolute, err := filepath.Abs(folder)
	if err != nil {
		return Owner{}, err
	}
	workspace, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Owner{}, err
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return Owner{}, err
	}
	if !info.IsDir() {
		return Owner{}, fmt.Errorf("project folder must be a directory")
	}
	root, err := fsutil.Path(workspace, ".devbox")
	return Owner{Kind: "project", Name: folder, Root: root, Workspace: workspace}, err
}
func (s Service) lock(ctx context.Context, root string) (*os.File, error) {
	dir, err := fsutil.Dir(s.Home, "state/locks/config", 0700)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(root))
	return fsutil.Lock(ctx, filepath.Join(dir, hex.EncodeToString(sum[:])+".lock"))
}
func readLayer(o Owner) ([]byte, config.Layer, error) {
	p, err := fsutil.Path(o.Root, "config.json")
	if err != nil {
		return nil, config.Layer{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, config.Layer{}, commanderror.New("owner_missing", fmt.Sprintf("Missing %s configuration.", o.Kind), p, err, o.step("create", "Create configuration"))
	}
	if err != nil {
		return nil, config.Layer{}, err
	}
	l, err := config.ParseLayer(b)
	if err != nil {
		err = fmt.Errorf("%s: %w", p, err)
	}
	return b, l, err
}
func patch(b []byte, key string, value any, remove bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := config.Decode(b, &fields); err != nil {
		return nil, err
	}
	if remove {
		delete(fields, key)
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = data
	}
	data, err := json.MarshalIndent(fields, "", "  ")
	return append(data, '\n'), err
}
func privateMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0700
	}
	return 0600
}

func (s Service) Create(ctx context.Context, o Owner, fromProfile string) (Result, error) {
	result := Result{Path: filepath.Join(o.Root, "config.json")}
	lock, err := s.lock(ctx, o.Root)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	if _, err = os.Lstat(o.Root); err == nil {
		return result, commanderror.New("owner_exists", "Configuration already exists.", o.Root, nil, o.step("init", "Initialize missing artifacts"))
	} else if !os.IsNotExist(err) {
		return result, err
	}
	files := map[string]harness.File{"config.json": {Data: []byte("{\n  \"version\": 1\n}\n"), Mode: 0600}}
	if fromProfile != "" {
		if o.Kind != "project" {
			return result, fmt.Errorf("from-profile is project-only")
		}
		source, err := s.Profile(fromProfile)
		if err != nil {
			return result, err
		}
		sourceLock, err := s.lock(ctx, source.Root)
		if err != nil {
			return result, err
		}
		defer fsutil.Unlock(sourceLock)
		registry, registryErr := harness.Enumerate(s.Home)
		if registryErr != nil {
			return result, registryErr
		}
		harnessNames := map[string]bool{}
		for _, entry := range registry.Valid {
			harnessNames[entry.Definition.Name] = true
		}
		for _, entry := range registry.Invalid {
			if config.Name.MatchString(entry.Name) {
				harnessNames[entry.Name] = true
			}
		}
		tree, err := artifact.SourceTree(source.Root, harnessNames)
		result.Warnings = append(result.Warnings, tree.Warnings...)
		if err != nil {
			return result, err
		}
		files = tree.Files
		data, err := patch(files["config.json"].Data, "inherit", false, false)
		if err != nil {
			return result, err
		}
		files["config.json"] = harness.File{Data: data, Mode: 0600}
	}
	parent, err := fsutil.Dir(filepath.Dir(o.Root), ".", 0700)
	if err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(parent, ".devbox-create-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	defer func() {
		for _, name := range names {
			if files[name].Mode.IsDir() {
				p, pathErr := fsutil.Path(stage, name)
				if pathErr == nil {
					_ = os.Chmod(p, 0700)
				}
			}
		}
	}()
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		p, err := fsutil.Path(stage, name)
		if err != nil {
			return result, err
		}
		if _, err = fsutil.Dir(stage, filepath.Dir(name), 0700); err != nil {
			return result, err
		}
		file := files[name]
		if file.Mode.IsDir() {
			if _, err = fsutil.Dir(stage, name, 0700); err != nil {
				return result, err
			}
			continue
		}
		if err = fsutil.Write(p, file.Data, file.Mode.Perm()); err != nil {
			return result, err
		}
	}
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		if files[name].Mode.IsDir() {
			if err = os.Chmod(filepath.Join(stage, name), files[name].Mode.Perm()); err != nil {
				return result, err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = fsutil.PublishDirectory(stage, o.Root); err != nil {
		return result, err
	}
	for _, name := range names {
		result.Created = append(result.Created, filepath.Join(o.Root, name))
	}
	result.Next = []commanderror.Step{o.step("init", "Select a harness and optional artifacts")}
	return result, nil
}

type Profile struct {
	Name    string `json:"name"`
	Harness string `json:"harness,omitempty"`
	Default bool   `json:"default"`
	Error   string `json:"error,omitempty"`
}

func (s Service) Profiles() ([]Profile, error) {
	global, _, err := s.global()
	if err != nil {
		return nil, err
	}
	root, err := fsutil.Path(s.Home, "profiles")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	profiles := []Profile{}
	for _, entry := range entries {
		if !config.Name.MatchString(entry.Name()) || (!entry.IsDir() && entry.Type()&os.ModeSymlink == 0) {
			continue
		}
		p := Profile{Name: entry.Name(), Default: global.DefaultProfile == entry.Name()}
		owner, err := s.Profile(entry.Name())
		if err == nil {
			var layer config.Layer
			_, layer, err = readLayer(owner)
			if layer.Harness != nil {
				p.Harness = *layer.Harness
			}
		}
		if err != nil {
			p.Error = err.Error()
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}
func (s Service) global() (config.Global, []byte, error) {
	p, err := fsutil.Path(s.Home, "config.json")
	if err != nil {
		return config.Global{}, nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return config.Global{}, nil, err
	}
	g, err := config.ParseGlobal(b)
	return g, b, err
}
func (s Service) SetDefault(ctx context.Context, name string) error {
	lock, err := s.lock(ctx, filepath.Join(s.Home, "config.json"))
	if err != nil {
		return err
	}
	defer fsutil.Unlock(lock)
	if name != "" {
		owner, err := s.Profile(name)
		if err != nil {
			return err
		}
		if _, _, err = readLayer(owner); err != nil {
			return err
		}
	}
	_, b, err := s.global()
	if err != nil {
		return err
	}
	b, err = patch(b, "default_profile", name, false)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return fsutil.Write(filepath.Join(s.Home, "config.json"), b, 0600)
}
