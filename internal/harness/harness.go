// Package harness loads declarative capabilities. Runtime code must not branch on names.
package harness

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"devbox/internal/config"
	"devbox/internal/fsutil"
)

//go:embed builtin/*/harness.json builtin/*/defaults/*
var builtins embed.FS

type Install struct {
	Shell string   `json:"shell"`
	Path  []string `json:"path"`
}
type Launch struct {
	Args     []string `json:"args"`
	Continue []string `json:"continue_args"`
}
type Store struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Target string `json:"target"`
}
type Config struct {
	Store string `json:"store"`
	Path  string `json:"path"`
}
type Merge struct {
	Path     string   `json:"path"`
	Strategy string   `json:"strategy"`
	Keys     []string `json:"owned_keys"`
}
type Auth struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Create bool   `json:"create"`
}
type Session struct {
	Preserve []string `json:"reset_preserve"`
	Relocate bool     `json:"relocate"`
	Clone    bool     `json:"clone"`
}
type Definition struct {
	Version int               `json:"version"`
	Name    string            `json:"name"`
	Binary  string            `json:"binary"`
	Install Install           `json:"install"`
	Launch  Launch            `json:"launch"`
	Env     map[string]string `json:"env"`
	Stores  []Store           `json:"stores"`
	Config  Config            `json:"config"`
	Merge   []Merge           `json:"config_merge"`
	Auth    []Auth            `json:"auth"`
	Session Session           `json:"session"`
	Prepare [][]string        `json:"prepare"`
}
type File struct {
	Data []byte
	Mode os.FileMode
}

type Effective struct {
	Definition Definition
	Origin     string
	Hash       string
	Defaults   map[string]File
}

func Load(home, name string) (Effective, error) {
	var result Effective
	if !config.Name.MatchString(name) {
		return result, fmt.Errorf("invalid harness name")
	}
	user, err := fsutil.Path(home, filepath.Join("harnesses", name, "harness.json"))
	if err != nil {
		return result, err
	}
	b, err := os.ReadFile(user)
	var defaults map[string]File
	origin := user
	if os.IsNotExist(err) {
		origin = "builtin"
		b, err = builtins.ReadFile("builtin/" + name + "/harness.json")
		if os.IsNotExist(err) {
			return result, fmt.Errorf("unknown harness %q", name)
		}
		defaults = map[string]File{}
		if err == nil {
			entries, e := builtins.ReadDir("builtin/" + name + "/defaults")
			if e != nil {
				return result, e
			}
			for _, entry := range entries {
				data, e := builtins.ReadFile("builtin/" + name + "/defaults/" + entry.Name())
				if e != nil {
					return result, e
				}
				info, e := entry.Info()
				if e != nil {
					return result, e
				}
				defaults[entry.Name()] = File{Data: data, Mode: info.Mode().Perm()}
			}
		}
	} else if err == nil {
		defaults, err = ReadTree(filepath.Join(filepath.Dir(user), "defaults"))
	}
	if err != nil {
		return result, err
	}
	def, err := parseDefinition(b)
	if err != nil {
		return result, fmt.Errorf("harness %s: invalid definition: %w", name, err)
	}
	if def.Name != name {
		return result, fmt.Errorf("harness definition name must match its directory")
	}
	sum := sha256.Sum256(b)
	return Effective{Definition: def, Origin: origin, Hash: hex.EncodeToString(sum[:]), Defaults: defaults}, nil
}
func parseDefinition(b []byte) (Definition, error) {
	d := Definition{Config: Config{Path: "."}}
	if err := config.Decode(b, &d); err != nil {
		return d, err
	}
	d.expandUser()
	return d, d.Validate()
}
func absolute(p string) bool {
	return path.IsAbs(p) && path.Clean(p) == p && !strings.ContainsAny(p, "\x00\r\n")
}
func relative(p string) bool { return p == "." || (filepath.IsLocal(p) && !strings.Contains(p, `\`)) }
func target(p string) bool {
	return absolute(p) && strings.HasPrefix(p, "/home/devuser/")
}
func (d Definition) Validate() error {
	if d.Version != 1 {
		return fmt.Errorf("unsupported version")
	}
	if !config.Name.MatchString(d.Name) || d.Binary == "" || strings.ContainsAny(d.Binary, "\x00\r\n") {
		return fmt.Errorf("invalid name or binary")
	}
	stores := map[string]Store{}
	targets := map[string]bool{}
	for _, s := range d.Stores {
		if !config.Name.MatchString(s.Name) || stores[s.Name].Name != "" {
			return fmt.Errorf("store names must be valid and unique")
		}
		if s.Scope != "environment" && s.Scope != "cache" {
			return fmt.Errorf("invalid store scope")
		}
		if !target(s.Target) {
			return fmt.Errorf("unsafe store target")
		}
		for other := range targets {
			if s.Target == other || strings.HasPrefix(s.Target, other+"/") || strings.HasPrefix(other, s.Target+"/") {
				return fmt.Errorf("overlapping store targets")
			}
		}
		stores[s.Name] = s
		targets[s.Target] = true
	}
	if stores[d.Config.Store].Scope != "environment" || !relative(d.Config.Path) {
		return fmt.Errorf("config must name an environment store and relative path")
	}
	paths := map[string]bool{}
	for _, m := range d.Merge {
		if !relative(m.Path) || m.Path == "." || m.Strategy != "json-keys" || !strings.HasSuffix(m.Path, ".json") || len(m.Keys) == 0 {
			return fmt.Errorf("invalid config_merge declaration")
		}
		for p := range paths {
			if p == m.Path || strings.HasPrefix(m.Path, p+"/") || strings.HasPrefix(p, m.Path+"/") {
				return fmt.Errorf("overlapping config_merge paths")
			}
		}
		paths[m.Path] = true
		keys := map[string]bool{}
		for _, k := range m.Keys {
			if k == "" || keys[k] {
				return fmt.Errorf("owned_keys must be non-empty and unique")
			}
			keys[k] = true
		}
	}
	for _, a := range d.Auth {
		if !relative(a.Source) || a.Source == "." || !target(a.Target) || (a.Kind != "file" && a.Kind != "directory") {
			return fmt.Errorf("invalid auth mount")
		}
		if targets[a.Target] {
			return fmt.Errorf("duplicate auth target")
		}
		targets[a.Target] = true
	}
	for _, argv := range d.Prepare {
		if len(argv) == 0 || argv[0] == "" {
			return fmt.Errorf("prepare must contain non-empty argv")
		}
	}
	for _, p := range d.Install.Path {
		if !absolute(p) {
			return fmt.Errorf("install path must be absolute and clean")
		}
	}
	for k := range d.Env {
		if !config.EnvName.MatchString(k) || strings.HasPrefix(k, "DEVBOX_") {
			return fmt.Errorf("invalid or reserved env name")
		}
	}
	for _, p := range d.Session.Preserve {
		if !relative(p) {
			return fmt.Errorf("reset_preserve must remain relative")
		}
	}
	return nil
}
func ReadTree(root string) (map[string]File, error) {
	files := map[string]File{}
	if _, err := fsutil.Path(root, "."); err != nil {
		return nil, err
	}
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if os.IsNotExist(err) && p == root {
			return nil
		}
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("only regular config files are supported: %s", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err == nil {
			files[filepath.ToSlash(rel)] = File{Data: b, Mode: info.Mode().Perm()}
		}
		return err
	})
	return files, err
}
