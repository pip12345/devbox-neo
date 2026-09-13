// Package harness loads declarative capabilities. Runtime code must not branch on names.
package harness

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"devbox/internal/commanderror"
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
	Relocate bool `json:"relocate"`
	Clone    bool `json:"clone"`
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

type Tree struct {
	Files    map[string]File
	Warnings []string
}

type Effective struct {
	Definition Definition
	Origin     string
	Hash       string
	Defaults   map[string]File
	Warnings   []string
}

func Load(home, name string) (result Effective, err error) {
	if !config.Name.MatchString(name) {
		return result, commanderror.New("invalid_harness", "Invalid harness name.", name, nil)
	}
	origin := filepath.Join(home, "harnesses", name, "harness.json")
	defer func() {
		var actionable *commanderror.Error
		if err != nil && !errors.As(err, &actionable) {
			err = commanderror.New("invalid_harness_definition", "Invalid harness definition: "+err.Error(), origin, err)
		}
	}()
	user, err := fsutil.Path(home, filepath.Join("harnesses", name, "harness.json"))
	if err != nil {
		return result, err
	}
	b, err := os.ReadFile(user)
	var defaults Tree
	origin = user
	if os.IsNotExist(err) {
		origin = "builtin"
		b, err = builtins.ReadFile("builtin/" + name + "/harness.json")
		if os.IsNotExist(err) {
			return result, commanderror.New("unknown_harness", "Unknown harness.", name, err)
		}
		if err == nil {
			var source fs.FS
			source, err = fs.Sub(builtins, "builtin/"+name+"/defaults")
			if err == nil {
				defaults, err = readTree(source, "builtin/"+name+"/defaults")
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
	return Effective{Definition: def, Origin: origin, Hash: hex.EncodeToString(sum[:]), Defaults: defaults.Files, Warnings: defaults.Warnings}, nil
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
func relative(p string) bool {
	return filepath.IsLocal(p) && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00\r\n")
}
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
		for _, s := range d.Stores {
			if strings.HasPrefix(s.Target, a.Target+"/") {
				return fmt.Errorf("auth mount cannot obscure a declared store")
			}
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
	return nil
}
func ReadTree(root string) (Tree, error) {
	if _, err := fsutil.Path(root, "."); err != nil {
		return Tree{}, err
	}
	return readTree(os.DirFS(root), root)
}

func readTree(source fs.FS, root string) (Tree, error) {
	tree := Tree{Files: map[string]File{}}
	err := fs.WalkDir(source, ".", func(p string, e fs.DirEntry, err error) error {
		if os.IsNotExist(err) && p == "." {
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
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("skipping non-regular config entry %q (%s); it will not be copied", filepath.Join(root, p), info.Mode().Type()))
			return nil
		}
		b, err := fs.ReadFile(source, p)
		if err == nil {
			tree.Files[p] = File{Data: b, Mode: info.Mode().Perm()}
		}
		return err
	})
	return tree, err
}
