// Package migration owns the removable old-Go-to-Neo importer. Ordinary runtime
// packages must not depend on it or interpret its journal.
package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"golang.org/x/sys/unix"
)

const journalVersion = 1

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var oldProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Paths has distinct immutable endpoints. Inherited DEVBOX_HOME is deliberately
// irrelevant: an old installation must never become the destination by accident.
type Paths struct{ Source, Destination, Work string }

func NewPaths(source, destination string) (Paths, error) {
	var p Paths
	for i, input := range []string{source, destination} {
		absolute, err := filepath.Abs(input)
		if err != nil || input == "" {
			return p, fmt.Errorf("source and destination paths are required")
		}
		if _, err = fsutil.Path(absolute, "."); err != nil {
			return p, err
		}
		if i == 0 {
			p.Source = absolute
		} else {
			p.Destination = absolute
		}
	}
	p.Work = p.Destination + ".migration"
	for _, pair := range [][2]string{{p.Source, p.Destination}, {p.Source, p.Work}, {p.Destination, p.Work}} {
		if within(pair[0], pair[1]) || within(pair[1], pair[0]) {
			return Paths{}, fmt.Errorf("source, destination, and staging must be separate, non-overlapping directories")
		}
	}
	if p.Source == "/" || p.Destination == "/" {
		return Paths{}, fmt.Errorf("a filesystem root cannot be a migration endpoint")
	}
	return p, nil
}
func oldContainerName(workspace, profile string) string {
	base := strings.ToLower(filepath.Base(workspace))
	var b strings.Builder
	for _, r := range base {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	folder := strings.Trim(b.String(), "-")
	if folder == "" {
		folder = "root"
	}
	sum := sha256.Sum256([]byte(workspace))
	name := "devbox-" + folder + "-" + hex.EncodeToString(sum[:4])
	if profile != "" {
		name += "-" + profile
	}
	return name
}
func (v *Inventory) directoryNames(root string) ([]string, error) {
	entries, err := readEntries(root)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		// Acquiring old-CLI locks may create state/ in a pre-label home. Its
		// installation identity is checked separately; bookkeeping is not payload.
		if root == v.Paths.Source && entry.Name() == "state" {
			continue
		}
		names = append(names, entry.Name()+":"+entry.Type().String())
	}
	return names, nil
}
func (v *Inventory) directory(root string) error {
	names, err := v.directoryNames(root)
	if err != nil {
		return err
	}
	v.Directories[root] = names
	return nil
}
func within(root, path string) bool {
	return root == path || strings.HasPrefix(path, root+string(filepath.Separator))
}
func supported(name string) bool { return name == "pi" || name == "opencode" }
func digest(b []byte) string     { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Item contains public inventory facts, never configuration or credential values.
// Keys are stable within a source snapshot and are also exact skip selectors.
type Item struct {
	Key           string   `json:"key"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	Harness       string   `json:"harness,omitempty"`
	Workspace     string   `json:"workspace,omitempty"`
	Profile       string   `json:"profile,omitempty"`
	SourceProfile string   `json:"source_profile,omitempty"`
	Target        string   `json:"target,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	Alias         string   `json:"alias,omitempty"`
	ClonedFrom    string   `json:"cloned_from,omitempty"`
	RelocatedFrom []string `json:"relocated_from,omitempty"`
	Created       string   `json:"created_at,omitempty"`
	Activity      string   `json:"last_activity,omitempty"`
	Action        string   `json:"last_action,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"`
	Issues        []string `json:"issues,omitempty"`
	Notices       []string `json:"notices,omitempty"`
	Changes       []string `json:"changes,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Bytes         int64    `json:"bytes"`
	Scanned       bool     `json:"scanned,omitempty"`
}

// File records copy paths, modes, and verification hashes. Generated config
// bytes stay in memory and staged files, never in the journal.
type File struct {
	Item     string      `json:"item"`
	Source   string      `json:"source"`
	Relative string      `json:"relative"`
	Hash     string      `json:"hash,omitempty"`
	Mode     os.FileMode `json:"mode"`
	Size     int64       `json:"size"`
	Data     []byte      `json:"-"`
	Link     string      `json:"link,omitempty"`
}
type Inventory struct {
	scans        []scanRequest
	progress     *preparationProgress
	Paths        Paths               `json:"paths"`
	Installation string              `json:"source_installation"`
	Items        []Item              `json:"items"`
	Files        []File              `json:"files"`
	SourceHashes map[string]string   `json:"source_hashes"`
	Directories  map[string][]string `json:"directories"`
	Notices      []string            `json:"notices"`
}

func (v *Inventory) item(key string) *Item {
	for i := range v.Items {
		if v.Items[i].Key == key {
			return &v.Items[i]
		}
	}
	return nil
}
func (v *Inventory) add(kind, name, path string) *Item {
	key := kind + ":" + name
	if old := v.item(key); old != nil {
		return old
	}
	v.Items = append(v.Items, Item{Key: key, Kind: kind, Name: name, Path: path})
	return &v.Items[len(v.Items)-1]
}
func (v *Inventory) issue(key, message string) {
	if i := v.item(key); i != nil {
		i.Issues = append(i.Issues, message)
	}
}
func (v *Inventory) notice(key, message string) {
	if i := v.item(key); i != nil {
		i.Notices = append(i.Notices, message)
	}
}
func (v *Inventory) depend(key, dep string) {
	if i := v.item(key); i != nil {
		for _, d := range i.Dependencies {
			if d == dep {
				return
			}
		}
		i.Dependencies = append(i.Dependencies, dep)
	}
}

// Private source schemas are normalized in source_migrations.go. No old startup
// loader or in-place repair runs against the source installation.
type oldGlobal struct {
	Version        int                          `json:"version"`
	DefaultProfile string                       `json:"default_profile"`
	DefaultHarness string                       `json:"default_harness"`
	GlobalEnv      []string                     `json:"global_env"`
	ProxyEnabled   bool                         `json:"proxy_enabled"`
	IgnoreProject  bool                         `json:"ignore_project_overrides"`
	Harnesses      map[string]map[string]string `json:"harnesses"`
}
type oldLayer struct {
	Version       int       `json:"version"`
	OnExit        *string   `json:"on_exit"`
	Shell         *[]string `json:"default_shell"`
	Harness       *string   `json:"harness"`
	HarnessArgs   []string  `json:"harness_args"`
	DockerArgs    []string  `json:"docker_args"`
	Mounts        []string  `json:"extra_mounts"`
	Env           []string  `json:"extra_env"`
	Ports         []string  `json:"extra_ports"`
	Networks      []string  `json:"extra_networks"`
	HostNetwork   *bool     `json:"host_network"`
	AutoRebuild   *bool     `json:"auto_rebuild"`
	LegacyProxy   *bool     `json:"proxy_enabled"`
	LegacyDomains *[]string `json:"proxy_allowed_domains"`
	Proxy         *struct {
		Enabled *bool `json:"enabled"`
		Allow   *struct {
			Domains []string `json:"domains"`
		} `json:"allow"`
	} `json:"proxy"`
	VSCode config.VSCode `json:"vscode"`
}
type oldRecord struct {
	Version       int      `json:"version"`
	ID            string   `json:"id"`
	Alias         string   `json:"alias"`
	Created       string   `json:"created_at"`
	Activity      string   `json:"last_activity_at"`
	Action        string   `json:"last_action"`
	ClonedFrom    string   `json:"cloned_from"`
	RelocatedFrom []string `json:"relocated_from"`
	Pending       *struct {
		Destination string `json:"destination"`
		Alias       string `json:"alias"`
	} `json:"pending_relocation"`
}
type oldMetadata struct {
	Version        int                                                      `json:"metadata_version"`
	Ownership      int                                                      `json:"ownership_version"`
	Name           string                                                   `json:"name"`
	Folder         string                                                   `json:"folder"`
	Profile        string                                                   `json:"profile"`
	Harness        string                                                   `json:"harness"`
	OnExit         string                                                   `json:"on_exit"`
	ProxyEnabled   bool                                                     `json:"proxy_enabled"`
	ProxyDomains   []string                                                 `json:"proxy_allowed_domains"`
	HostNetwork    bool                                                     `json:"host_network"`
	ReadOnly       bool                                                     `json:"read_only"`
	Mounts         []string                                                 `json:"extra_mounts"`
	Env            []string                                                 `json:"extra_env"`
	Ports          []string                                                 `json:"extra_ports"`
	Networks       []string                                                 `json:"extra_networks"`
	DockerArgs     []string                                                 `json:"docker_args"`
	HarnessArgs    []string                                                 `json:"harness_args"`
	ImageTag       string                                                   `json:"image_tag"`
	ImageDigest    string                                                   `json:"image_digest"`
	ArtifactsHash  string                                                   `json:"artifacts_hash"`
	Manifest       []struct{ Name, Path, Kind, Hash, Source, Owner string } `json:"artifact_manifest"`
	BundledAssets  string                                                   `json:"bundled_assets_hash"`
	BundledImage   string                                                   `json:"bundled_image_assets_hash"`
	BundledRuntime string                                                   `json:"bundled_runtime_assets_hash"`
	Creation       *struct {
		Profile       string   `json:"profile"`
		Harness       string   `json:"harness"`
		ReadOnly      bool     `json:"read_only"`
		HostNetwork   bool     `json:"host_network"`
		Mounts        []string `json:"extra_mounts"`
		HarnessMounts []string `json:"harness_mounts"`
		Env           []string `json:"extra_env"`
		Ports         []string `json:"extra_ports"`
		DockerArgs    []string `json:"docker_args"`
		Proxy         struct {
			Enabled bool     `json:"enabled"`
			Domains []string `json:"allowed_domains"`
		} `json:"proxy"`
	} `json:"creation_settings"`
	Created string `json:"created_at"`
}

func readRegular(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := fsutil.Path(filepath.Dir(path), filepath.Base(path)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular file: %s", path)
	}
	return os.ReadFile(path)
}
func (v *Inventory) read(ctx context.Context, path string) ([]byte, error) {
	b, err := readRegular(ctx, path)
	if err == nil {
		v.SourceHashes[path] = digest(b)
	} else if os.IsNotExist(err) {
		v.SourceHashes[path] = "missing"
	}
	return b, err
}
func encode(value any) []byte { b, _ := json.MarshalIndent(value, "", "  "); return append(b, '\n') }
func convertLayer(b []byte) (config.Layer, []string, error) {
	old, notices, err := sourceLayer(b)
	if err != nil {
		return config.Layer{}, nil, err
	}
	if len(old.Networks) > 0 {
		return config.Layer{}, nil, fmt.Errorf("extra_networks requires manual review: Neo has one primary network")
	}
	l := config.Layer{Version: 1, Shell: old.Shell, Harness: old.Harness, HarnessArgs: old.HarnessArgs, DockerArgs: old.DockerArgs, Mounts: old.Mounts, Env: old.Env, Ports: old.Ports, VSCode: old.VSCode}
	if old.OnExit != nil {
		notices = append(notices, "Removed on_exit; manual start keeps sessions running until stop, including across reboot.")
	}
	if old.HostNetwork != nil {
		network := "default"
		if *old.HostNetwork {
			network = "host"
		}
		l.Network = &network
	}
	if l.Harness != nil && *l.Harness != "" && !supported(*l.Harness) {
		return l, notices, fmt.Errorf("unsupported harness %s; manually review/fix or skip this owner and dependent sessions", strconv.QuoteToASCII(*l.Harness))
	}
	// Parse the raw result before host expansion: expressions remain expressions.
	if _, err := config.ParseLayer(encode(l)); err != nil {
		return l, notices, fmt.Errorf("converted layer does not match Neo's schema: %w", err)
	}
	return l, notices, nil
}
func (v *Inventory) generated(key, source, rel string, b []byte, mode os.FileMode) {
	v.Files = append(v.Files, File{Item: key, Source: source, Relative: rel, Hash: digest(b), Mode: mode, Size: int64(len(b)), Data: b})
}

// InventorySource discovers owners and validates their metadata without walking
// payload trees or reading credentials/history/caches. Selected payloads acquire
// their immutable snapshot only after approval, under the source writer locks.
func InventorySource(ctx context.Context, p Paths) (*Inventory, error) {
	checked, err := NewPaths(p.Source, p.Destination)
	if err != nil {
		return nil, err
	}
	if checked != p {
		return nil, fmt.Errorf("invalid staging path")
	}
	if info, err := os.Stat(p.Source); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("source home must be an existing directory")
	}
	v := &Inventory{Paths: p, SourceHashes: map[string]string{}, Directories: map[string][]string{}, Notices: []string{"Original home and Docker resources are retained in place.", "Workspace contents, external mounts/volumes, and arbitrary container-layer files are not backed up.", "Caches are excluded unless explicitly selected.", "Aliases and permanent lineage are report-only; they do not become Neo commands."}}
	identityPath := filepath.Join(p.Source, "state/installation-id")
	b, err := v.read(ctx, identityPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read source installation identity: %w", err)
	}
	v.Installation = strings.TrimSpace(string(b))
	if err == nil && !idPattern.MatchString(v.Installation) {
		return nil, fmt.Errorf("invalid source installation identity")
	}
	globalPath := filepath.Join(p.Source, "global.json")
	key := v.add("global", "config", globalPath).Key
	b, err = v.read(ctx, globalPath)
	if os.IsNotExist(err) {
		b, err = []byte(`{}`), nil
		v.notice(key, "No global.json exists; converting the old CLI's built-in global defaults without creating source files.")
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read source global configuration: %w", err)
	}
	g, err := sourceGlobal(b)
	if err != nil {
		return nil, err
	}
	ng := config.Global{Version: 1, DefaultProfile: g.DefaultProfile, DefaultHarness: g.DefaultHarness, GlobalEnv: g.GlobalEnv, IgnoreProject: g.IgnoreProject}
	if g.DefaultHarness != "" && !supported(g.DefaultHarness) {
		v.issue(key, "Unsupported default harness; manually select Pi/OpenCode before staging global configuration.")
	}
	if g.DefaultProfile != "" {
		v.depend(key, "profile:"+g.DefaultProfile)
	}
	if g.ProxyEnabled {
		v.item(key).Changes = append(v.item(key).Changes, "Proxy protection will not carry over; Neo does not restrict container egress.")
	}
	if v.Installation == "" {
		v.notice(key, "Source has no installation ID; only metadata-backed pre-label sessions can be imported. No source identity will be created.")
	}
	v.notice(key, "Converted global.json to Neo config.json version 1; supported defaults and env expressions are retained.")
	v.generated(key, globalPath, "config.json", encode(ng), 0600)
	profiles, err := readEntries(filepath.Join(p.Source, "profiles"))
	if err != nil {
		return nil, err
	}
	for _, entry := range profiles {
		root := filepath.Join(p.Source, "profiles", entry.Name())
		item := v.add("profile", entry.Name(), root)
		key := item.Key
		if !entry.IsDir() || !oldProfilePattern.MatchString(entry.Name()) {
			v.issue(key, "Invalid source profile directory; manual review required.")
			continue
		}
		if !config.Name.MatchString(entry.Name()) {
			v.notice(key, "Profile name is not valid in Neo; explicitly rename it during merge review.")
		}
		v.inventoryLayer(ctx, key, root, filepath.Join("profiles", entry.Name()), g.DefaultHarness, false)
	}
	sessions, err := readEntries(filepath.Join(p.Source, "sessions"))
	if err != nil {
		return nil, err
	}
	seenIDs := map[string]string{}
	for _, entry := range sessions {
		root := filepath.Join(p.Source, "sessions", entry.Name())
		key := v.add("session", entry.Name(), root).Key
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "devbox-") {
			v.issue(key, "Invalid source session directory.")
			continue
		}
		if err := v.directory(root); err != nil {
			return nil, err
		}
		rb, rerr := v.read(ctx, filepath.Join(root, "session.json"))
		mb, merr := v.read(ctx, filepath.Join(root, "metadata.json"))
		var r oldRecord
		var m oldMetadata
		for _, source := range []struct {
			name    string
			data    []byte
			readErr error
			value   any
		}{
			{"session.json", rb, rerr, &r},
			{"metadata.json", mb, merr, &m},
		} {
			if source.name == "session.json" && os.IsNotExist(source.readErr) {
				continue
			}
			if source.readErr != nil {
				v.issue(key, "Cannot read "+source.name+": "+filesystemDiagnostic(source.readErr)+".")
			} else if err := config.Decode(source.data, source.value); err != nil {
				v.issue(key, "Invalid "+source.name+": "+schemaDiagnostic(err)+".")
			}
		}
		if len(v.item(key).Issues) > 0 {
			continue
		}
		if os.IsNotExist(rerr) {
			r, err = v.missingRecord(root, m)
			if err != nil {
				v.issue(key, "Cannot establish creation time for missing session record.")
			}
			v.notice(key, "Source has no session.json; a stable import ID is assigned without modifying the source. No historical activity is invented.")
		} else if problem := versionDiagnostic(rb, "version", r.Version, 1); problem != "" {
			v.issue(key, "session.json: "+problem+".")
		}
		for _, problem := range metadataProblems(mb, m) {
			v.issue(key, problem)
		}
		if m.Ownership == 1 && v.Installation == "" {
			v.issue(key, "Labeled source metadata requires the source state/installation-id; restore it before import.")
		}
		if m.Ownership == 0 {
			v.notice(key, "Pre-label source ownership will be verified against matching host metadata; containers are never adopted.")
		}
		if len(v.item(key).Issues) > 0 {
			continue
		}
		if m.Creation.Profile != m.Profile || m.Creation.Harness != m.Harness {
			v.issue(key, "Creation settings disagree with session profile/harness metadata.")
		}
		if !idPattern.MatchString(r.ID) || m.Name != entry.Name() || !filepath.IsAbs(m.Folder) || filepath.Clean(m.Folder) != m.Folder {
			v.issue(key, "Invalid session identity or workspace metadata.")
			continue
		}
		if previous := seenIDs[r.ID]; previous != "" {
			v.issue(key, "Duplicate source session ID.")
			v.issue(previous, "Duplicate source session ID.")
		} else {
			seenIDs[r.ID] = key
		}
		created, err := time.Parse(time.RFC3339Nano, r.Created)
		if err != nil || created.IsZero() {
			v.issue(key, "Invalid source creation timestamp.")
		}
		if r.Activity != "" {
			if _, err := time.Parse(time.RFC3339Nano, r.Activity); err != nil {
				v.issue(key, "Invalid source activity timestamp.")
			}
		}
		if r.Pending != nil {
			v.issue(key, "Pending relocation; complete/repair it with the old CLI or skip.")
		}
		item := v.item(key)
		item.Harness = m.Harness
		item.Workspace = m.Folder
		item.SourceProfile = m.Profile
		item.Profile = m.Profile
		projectSlot := strings.HasSuffix(m.Name, "-.project")
		suffix := m.Profile
		if projectSlot {
			item.Profile = ""
			suffix = ".project"
		}
		if oldContainerName(m.Folder, suffix) != m.Name {
			v.issue(key, "Source container name does not match the recorded workspace/slot.")
		}
		item.SessionID = r.ID
		item.Alias = r.Alias
		item.Created = r.Created
		item.Activity = r.Activity
		item.Action = r.Action
		item.ClonedFrom = r.ClonedFrom
		item.RelocatedFrom = r.RelocatedFrom
		if !supported(m.Harness) {
			v.issue(key, "Unsupported recorded harness; changing configuration does not convert these conversations.")
		}
		if m.Profile != "" {
			v.depend(key, "profile:"+m.Profile)
		}
		v.depend(key, "auth:"+m.Harness)
		if problem := workspaceDiagnostic(m.Folder); problem != "" {
			v.issue(key, problem+" Review the recorded path; repair/relocate with the old CLI or explicitly skip.")
		}
		projectRoot := filepath.Join(m.Folder, ".devbox")
		if st, e := os.Lstat(projectRoot); e == nil {
			projectKey := "project:" + m.Folder
			if v.item(projectKey) == nil {
				v.add("project", m.Folder, projectRoot)
				overlap := false
				for _, endpoint := range []string{p.Source, p.Destination, p.Work} {
					if within(endpoint, projectRoot) || within(projectRoot, endpoint) {
						overlap = true
					}
				}
				if overlap {
					v.issue(projectKey, "Project artifacts overlap a migration endpoint; manual review is required.")
				} else if !st.IsDir() {
					v.issue(projectKey, "Project artifact root is not a directory.")
				} else {
					v.inventoryLayer(ctx, projectKey, projectRoot, filepath.Join("projects", digest([]byte(m.Folder))), g.DefaultHarness, true)
				}
			}
			if projectSlot || !g.IgnoreProject {
				v.depend(key, projectKey)
				if g.DefaultProfile != "" && !g.IgnoreProject {
					v.depend(key, "profile:"+g.DefaultProfile)
				}
			}
		} else if !os.IsNotExist(e) {
			v.issue(key, "Cannot inspect project artifacts.")
		}
		if projectSlot && v.item("project:"+m.Folder) == nil {
			v.issue(key, "Project-slot session has no project artifacts; manual review required.")
		}
		projectParticipates := projectSlot || (v.item("project:"+m.Folder) != nil && !g.IgnoreProject)
		slot := environment.Slot(m.Profile, projectParticipates)
		v.item(key).Target = environment.ContainerName(m.Folder, slot)
		if m.ProxyEnabled {
			v.notice(key, "Proxy protection will not carry over.")
		}
		if len(m.Networks) > 0 {
			v.issue(key, "Recorded extra_networks requires manual review.")
		}
		v.notice(key, "Rebuild runs setup hooks; shared workspace and external resources are not rolled back.")
		v.notice(key, "Recorded creation settings must be compared with final configuration before merge; values are withheld from this report.")
		if supported(m.Harness) {
			v.inventorySessionState(ctx, key, root, v.item(key).Target, m.Harness)
		}
	}
	for _, h := range []string{"pi", "opencode"} {
		root := filepath.Join(p.Source, "auth", h)
		key := v.add("auth", h, root).Key
		v.item(key).Harness = h
		source := filepath.Join(root, "auth.json")
		if settings := g.Harnesses[h]; settings != nil {
			for field, value := range settings {
				if field != "auth_file" && value != "" {
					v.issue(key, "Unsupported harness auth setting: "+strconv.QuoteToASCII(field))
				}
			}
			if external := settings["auth_file"]; external != "" {
				if strings.HasPrefix(external, "~/") {
					home, e := os.UserHomeDir()
					if e != nil {
						return nil, e
					}
					external = filepath.Join(home, external[2:])
				}
				if !filepath.IsAbs(external) {
					v.issue(key, "External auth source must be an absolute path.")
					continue
				}
				source = external
				v.item(key).Path = source
				v.notice(key, "External authentication will be copied only with explicit approval; original remains untouched.")
			}
		}
		// Managed auth can itself be a deliberate old Devbox link to external auth.
		if info, e := os.Lstat(source); e == nil && info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(source)
			if e != nil {
				return nil, e
			}
			v.SourceHashes[source] = "link:" + digest([]byte(link))
			resolved, e := filepath.EvalSymlinks(source)
			if e != nil {
				v.issue(key, "Authentication link cannot be resolved.")
				continue
			}
			source = resolved
			v.item(key).Path = source
			v.notice(key, "External authentication will be copied only with explicit approval; original remains untouched.")
		}
		if _, e := os.Lstat(source); os.IsNotExist(e) {
			v.SourceHashes[source] = "missing"
			v.notice(key, "No source credentials exist; authentication may be needed after import.")
			continue
		}
		if _, e := fsutil.Path(filepath.Dir(source), filepath.Base(source)); e != nil {
			v.issue(key, "Cannot safely inventory authentication source.")
		} else if info, e := os.Lstat(source); e != nil || !info.Mode().IsRegular() {
			v.issue(key, "Authentication source must be an accessible regular file.")
		} else {
			v.scans = append(v.scans, scanRequest{item: key, source: source, relative: filepath.Join("auth", h, "auth.json")})
		}
	}
	for _, h := range []string{"pi", "opencode"} {
		root := filepath.Join(p.Source, "cache/harnesses", h)
		entries, e := readEntries(root)
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			key := v.add("cache", h+"/"+entry.Name(), filepath.Join(root, entry.Name())).Key
			if h != "pi" || (entry.Name() != "npm-global" && entry.Name() != "npm-cache") {
				v.issue(key, "No tested mapping for this optional cache.")
				continue
			}
			v.scans = append(v.scans, scanRequest{item: key, source: filepath.Join(root, entry.Name()), relative: filepath.Join("cache/harnesses", h, entry.Name()), tree: true})
		}
	}
	entries, err := readEntries(p.Source)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{"global.json": true, "profiles": true, "sessions": true, "auth": true, "cache": true, "state": true, "proxy-allow.json": true}
	for _, entry := range entries {
		if !known[entry.Name()] {
			v.Notices = append(v.Notices, "Not imported: "+filepath.Join(p.Source, entry.Name()))
		}
	}
	for _, category := range []string{"auth", "cache/harnesses"} {
		entries, e := readEntries(filepath.Join(p.Source, category))
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			root := filepath.Join(p.Source, category, entry.Name())
			if !supported(entry.Name()) {
				v.Notices = append(v.Notices, "Not imported: "+root)
				continue
			}
			if category == "auth" && entry.IsDir() {
				children, e := readEntries(root)
				if e != nil {
					return nil, e
				}
				for _, child := range children {
					if child.Name() != "auth.json" {
						v.Notices = append(v.Notices, "Not imported: "+filepath.Join(root, child.Name()))
					}
				}
			}
		}
	}
	v.Notices = append(v.Notices, "Old state/, leases, proxy-allow.json, proxy certificates, and generated bookkeeping are not imported.")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Owner membership is reviewed up front. Selected payload-directory membership
	// is captured later, with its file hashes, before any data is copied.
	for _, root := range []string{p.Source, filepath.Join(p.Source, "profiles"), filepath.Join(p.Source, "sessions")} {
		if err := v.directory(root); err != nil {
			return nil, err
		}
	}
	sort.Slice(v.Items, func(i, j int) bool { return v.Items[i].Key < v.Items[j].Key })
	sort.Slice(v.Files, func(i, j int) bool { return v.Files[i].Relative < v.Files[j].Relative })
	return v, nil
}
func readEntries(path string) ([]os.DirEntry, error) {
	if _, err := fsutil.Path(path, "."); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return entries, err
}
func (v *Inventory) inventoryLayer(ctx context.Context, key, root, rel, defaultHarness string, project bool) {
	b, err := v.read(ctx, filepath.Join(root, "config.json"))
	if os.IsNotExist(err) {
		b = []byte(`{"version":1}`)
		err = nil
	}
	if err != nil {
		v.issue(key, "Cannot read source config.json.")
		return
	}
	l, notices, err := convertLayer(b)
	for _, n := range notices {
		v.item(key).Changes = append(v.item(key).Changes, n)
	}
	if err != nil {
		v.issue(key, err.Error())
		return
	}
	h := defaultHarness
	if l.Harness != nil && *l.Harness != "" {
		h = *l.Harness
	}
	v.item(key).Harness = h
	if h != "" && !supported(h) {
		v.issue(key, "Unsupported effective harness; manually review/fix or skip.")
	}
	if _, e := os.Lstat(filepath.Join(root, "Dockerfile.full")); e == nil {
		v.issue(key, "Dockerfile.full is unsupported; supply a layered Dockerfile or skip.")
	} else if !os.IsNotExist(e) {
		v.issue(key, "Cannot inspect Dockerfile.full.")
	}
	if project {
		v.notice(key, "Project config conversion requires separate merge approval and an external backup.")
	}
	v.generated(key, filepath.Join(root, "config.json"), filepath.Join(rel, "config.json"), encode(l), 0600)
	skip := map[string]bool{"config.json": true, "Dockerfile.full": true}
	v.scans = append(v.scans, scanRequest{item: key, source: root, relative: rel, tree: true, skip: skip})
}
func (v *Inventory) inventorySessionState(ctx context.Context, key, root, target, active string) {
	layout, err := inspectSourceLayout(root)
	if err != nil {
		v.issue(key, err.Error())
		return
	}
	for path, link := range layout.aliases {
		v.SourceHashes[path] = "link:" + digest([]byte(link))
	}
	for path, names := range layout.directories {
		v.Directories[path] = names
	}
	for _, name := range sourceHarnessNames {
		source := layout.stores[name]
		if source == "" {
			continue
		}
		if !supported(name) {
			v.item(key).Warnings = append(v.item(key).Warnings, fmt.Sprintf("Not imported: retained %s harness state at %q. Only Pi/OpenCode stores are copied; this store and any conversations in it remain untouched in the old installation.", name, source))
			continue
		}
		storeName := "home"
		if name == "opencode" {
			storeName = "data"
		}
		rel := filepath.Join("sessions", target, "harnesses", name, "stores", storeName)
		v.scans = append(v.scans, scanRequest{item: key, source: source, relative: rel, projection: filepath.Join(layout.projection, name), tree: true, skip: map[string]bool{"auth.json": true}})
		v.depend(key, "auth:"+name)
	}
	if layout.stores[active] == "" {
		v.issue(key, "Recorded harness state directory is missing.")
	}
	if active == "opencode" {
		v.notice(key, "OpenCode's separate container-only config must be captured during merge review, or explicitly omitted.")
	}
}
func (v *Inventory) file(ctx context.Context, key, source, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("unsafe target path")
	}
	if _, err := fsutil.Path(filepath.Dir(source), filepath.Base(source)); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected regular file: %s", source)
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	v.progress.file(source)
	n, err := io.Copy(h, v.progress.reader(ctx, f))
	if err != nil {
		return err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	v.SourceHashes[source] = hash
	v.Files = append(v.Files, File{Item: key, Source: source, Relative: relative, Hash: hash, Mode: privateMode(info.Mode()), Size: n})
	v.item(key).Bytes += n
	return nil
}
func privateMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0700
	}
	return 0600
}
func (v *Inventory) tree(ctx context.Context, key, root, rel string, skip map[string]bool, projection string) error {
	return v.projectedTree(ctx, key, root, rel, skip, projection, map[string]bool{})
}

func (v *Inventory) projectedTree(ctx context.Context, key, root, rel string, skip map[string]bool, projection string, visiting map[string]bool) error {
	if visiting[root] {
		return fmt.Errorf("cyclic generated config projection: %s", root)
	}
	visiting[root] = true
	defer delete(visiting, root)
	if _, err := fsutil.Path(root, "."); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		local, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if skip[local] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(rel, local)
		if entry.IsDir() {
			if err := v.directory(path); err != nil {
				return err
			}
			v.Files = append(v.Files, File{Item: key, Source: path, Relative: target, Mode: os.ModeDir | 0700})
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Relative internal links retain their meaning. Generated container links
			// require their own proven mapping, never traversal through a guessed host path.
			v.SourceHashes[path] = "link:" + digest([]byte(link))
			if projection != "" && (link == "/devbox/harness-config" || strings.HasPrefix(link, "/devbox/harness-config/")) {
				entryRelative := local
				local := strings.TrimPrefix(link, "/devbox/harness-config/")
				if link == "/devbox/harness-config" {
					local = "."
				}
				if !filepath.IsLocal(local) {
					return fmt.Errorf("generated config link %s -> %s escapes its config root; no host mapping attempted", strconv.QuoteToASCII(path), strconv.QuoteToASCII(link))
				}
				stagedRoot := projection
				mapped := filepath.Join(stagedRoot, local)
				if _, err := fsutil.Path(stagedRoot, local); err != nil {
					return fmt.Errorf("generated config link %s -> %s; mapped host path %s rejected: %s", strconv.QuoteToASCII(path), strconv.QuoteToASCII(link), strconv.QuoteToASCII(mapped), filesystemDiagnostic(err))
				}
				info, err := os.Stat(mapped)
				if err != nil {
					// Only the live home's mirrored links are generated. Links
					// inside staged user content are not disposable projections.
					if os.IsNotExist(err) && staleProjection(stagedRoot, entryRelative, link) && !within(projection, root) {
						if err := v.directory(stagedRoot); err != nil {
							return err
						}
						v.SourceHashes[mapped] = "missing"
						v.item(key).Changes = append(v.item(key).Changes, projectionOmission(path, link))
						return nil
					}
					return fmt.Errorf("generated config link %s -> %s; mapped host path %s unavailable: %s", strconv.QuoteToASCII(path), strconv.QuoteToASCII(link), strconv.QuoteToASCII(mapped), filesystemDiagnostic(err))
				}
				if info.IsDir() {
					return v.projectedTree(ctx, key, mapped, target, nil, projection, visiting)
				}
				return v.file(ctx, key, mapped, target)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || filepath.IsAbs(link) || !filepath.IsLocal(filepath.Join(filepath.Dir(local), link)) || !within(root, resolved) {
				return fmt.Errorf("unresolved or escaping symlink: %s", path)
			}
			resolvedRelative, _ := filepath.Rel(root, resolved)
			if skip[resolvedRelative] {
				return fmt.Errorf("symlink targets excluded state: %s", path)
			}
			v.SourceHashes[path] = "link:" + digest([]byte(link))
			v.Files = append(v.Files, File{Item: key, Source: path, Relative: target, Hash: digest([]byte(link)), Mode: os.ModeSymlink, Link: link})
			return nil
		}
		return v.file(ctx, key, path, target)
	})
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// Selection records every exclusion, including dependency-driven exclusions.
// Missing decisions are blockers rather than implicit loss of user data.
type Selection struct {
	Skip         []string `json:"skip"`
	Caches       bool     `json:"caches"`
	ExternalAuth []string `json:"external_auth"`
}

func (v *Inventory) Select(s Selection) (map[string]string, error) {
	excluded := map[string]string{}
	for _, key := range s.Skip {
		if v.item(key) == nil {
			return nil, fmt.Errorf("unknown skip selector: %s", strconv.QuoteToASCII(key))
		}
		excluded[key] = "explicitly skipped"
	}
	for _, i := range v.Items {
		if i.Kind == "cache" && !s.Caches {
			excluded[i.Key] = "optional caches not selected"
		}
	}
	for changed := true; changed; {
		changed = false
		for _, i := range v.Items {
			if excluded[i.Key] != "" {
				continue
			}
			for _, dep := range i.Dependencies {
				if excluded[dep] != "" {
					excluded[i.Key] = "dependency skipped: " + dep
					changed = true
					break
				}
			}
		}
	}
	for _, i := range v.Items {
		if excluded[i.Key] != "" {
			continue
		}
		if len(i.Issues) > 0 {
			return excluded, fmt.Errorf("%s needs manual review or explicit skip", i.Key)
		}
		for _, dep := range i.Dependencies {
			if v.item(dep) == nil {
				return excluded, fmt.Errorf("%s requires missing %s", i.Key, dep)
			}
		}
		if i.Kind == "auth" && !within(v.Paths.Source, i.Path) && !contains(s.ExternalAuth, i.Key) {
			return excluded, fmt.Errorf("%s requires explicit approval to copy external authentication", i.Key)
		}
	}
	return excluded, nil
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// SourceRuntime supplies read-only Docker inspection during preparation.
// Tests supply a fake; no test needs access to a personal daemon.
type SourceRuntime interface {
	CheckIdle(context.Context, *Inventory, map[string]string) error
}
type DockerSource struct{ Runtime docker.Runtime }

func (d DockerSource) CheckIdle(ctx context.Context, v *Inventory, excluded map[string]string) (err error) {
	// Inspection errors below contain only public identity facts. Mark those
	// messages as public while keeping arbitrary runner failures private.
	defer func() {
		var public *commanderror.Error
		if err != nil && !errors.As(err, &public) {
			err = commanderror.New("migration_source_preflight", err.Error(), v.Paths.Source, err)
		}
	}()
	if d.Runtime.Runner == nil {
		return fmt.Errorf("Docker inspection is unavailable")
	}
	// Auth and caches are shared across source sessions. A skipped or unrecorded
	// installation-owned container must not keep writing them while we copy.
	var ids bytes.Buffer
	if v.Installation != "" {
		if err := d.Runtime.Runner.Run(ctx, docker.Command{Args: []string{"container", "ls", "--all", "--filter", "label=devbox.managed=true", "--filter", "label=devbox.installation_id=" + v.Installation, "--format", "{{.ID}}"}, Stdout: &ids}); err != nil {
			return commanderror.New("migration_source_unavailable", "Cannot inventory source Docker resources.", v.Paths.Source, err)
		}
	}
	for _, id := range strings.Fields(ids.String()) {
		if !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(id) {
			return fmt.Errorf("invalid source Docker inventory")
		}
		var output bytes.Buffer
		if err := d.Runtime.Runner.Run(ctx, docker.Command{Args: []string{"container", "inspect", id}, Stdout: &output}); err != nil {
			return commanderror.New("migration_source_changed", "Source Docker inventory changed; recheck.", v.Paths.Source, err)
		}
		var list []docker.Container
		if json.Unmarshal(output.Bytes(), &list) != nil || len(list) != 1 || !strings.HasPrefix(list[0].ID, id) {
			return fmt.Errorf("invalid source Docker inspection")
		}
		c := list[0]
		if c.Config.Labels["devbox.managed"] != "true" || c.Config.Labels["devbox.installation_id"] != v.Installation {
			return fmt.Errorf("source Docker ownership changed; recheck")
		}
		if c.State.Running {
			return fmt.Errorf("source container is running: %s; stop it with the old Devbox CLI and recheck", strings.TrimPrefix(c.Name, "/"))
		}
	}
	for _, i := range v.Items {
		if i.Kind != "session" {
			continue
		}
		// Legacy writers do not appear in the installation-label inventory.
		// Check them even when excluded: auth and caches are shared.
		for _, name := range []string{i.Name, "devbox-proxy-" + i.Name} {
			c, exists, err := d.Runtime.Inspect(ctx, name)
			if err != nil {
				return commanderror.New("migration_source_unavailable", fmt.Sprintf("Cannot verify source Docker state for %s.", i.Name), i.Name, err)
			}
			if !exists {
				continue
			}
			if excluded[i.Key] != "" && !c.State.Running {
				continue
			}
			if err := verifySourceContainer(ctx, v, i, c, name != i.Name); err != nil {
				return err
			}
			if c.State.Running {
				return fmt.Errorf("source container is running: %s; stop it with the old Devbox CLI and recheck", name)
			}
		}
	}
	return nil
}
func checkLeases(ctx context.Context, v *Inventory) error {
	for _, i := range v.Items {
		if i.Kind != "session" {
			continue
		}
		for _, leaseDir := range sourceLeasePaths(i.Path) {
			entries, err := readEntries(leaseDir)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				b, err := readRegular(ctx, filepath.Join(leaseDir, entry.Name()))
				if err != nil {
					return fmt.Errorf("cannot verify source lease: %s", i.Name)
				}
				var lease struct {
					PID     int    `json:"pid"`
					Started string `json:"started_at"`
				}
				if json.Unmarshal(b, &lease) != nil || lease.PID <= 0 {
					return fmt.Errorf("invalid source lease; review with old CLI: %s", i.Name)
				}
				err = syscall.Kill(lease.PID, 0)
				if err == nil || errors.Is(err, syscall.EPERM) {
					return fmt.Errorf("source has active attached commands: %s", i.Name)
				}
				if !errors.Is(err, syscall.ESRCH) {
					return fmt.Errorf("cannot verify source process: %s", i.Name)
				}
			}
		}
	}
	return nil
}
func lockSource(ctx context.Context, v *Inventory) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var locks []*os.File
	release := func() {
		for n := len(locks) - 1; n >= 0; n-- {
			_ = fsutil.Unlock(locks[n])
		}
	}
	dir, err := fsutil.Dir(v.Paths.Source, "state/locks/sessions", 0700)
	if err != nil {
		return release, err
	}
	paths := sourceLockPaths(v, dir)
	for _, path := range paths {
		f, err := fsutil.Lock(ctx, path)
		if err != nil {
			release()
			return func() {}, err
		}
		locks = append(locks, f)
	}
	return release, nil
}

// The old runtime orders multi-session operation locks by container name, not
// by their hashed filenames. Acquire every operation lock before record locks
// so an old transfer cannot deadlock with this offline snapshot.
func sourceLockPaths(v *Inventory, dir string) []string {
	names := []string{}
	for _, i := range v.Items {
		if i.Kind == "session" {
			names = append(names, i.Name)
		}
	}
	sort.Strings(names)
	paths := []string{}
	for _, kind := range []string{"operation", "record"} {
		for _, name := range names {
			sum := sha256.Sum256([]byte(name))
			key := hex.EncodeToString(sum[:16])
			paths = append(paths, filepath.Join(dir, key+"."+kind+".lock"))
		}
	}
	return paths
}

func checkRuntime(ctx context.Context, runtime SourceRuntime, v *Inventory, excluded map[string]string) error {
	err := runtime.CheckIdle(ctx, v, excluded)
	if err == nil {
		return nil
	}
	var public *commanderror.Error
	if errors.As(err, &public) {
		return err
	}
	return commanderror.New("migration_source_check_failed", "Source Docker check failed; verify that the source installation is stopped and accessible.", v.Paths.Source, err)
}

type Journal struct {
	PlannedInstallation string             `json:"planned_installation,omitempty"`
	Captures            map[string]Capture `json:"captures,omitempty"`
	Merge               *MergeState        `json:"merge,omitempty"`
	Version             int                `json:"version"`
	ID                  string             `json:"id"`
	Started             time.Time          `json:"started"`
	Updated             time.Time          `json:"updated"`
	Phase               string             `json:"phase"`
	Inventory           Inventory          `json:"inventory"`
	Selection           Selection          `json:"selection"`
	Excluded            map[string]string  `json:"excluded"`
	Completed           map[string]string  `json:"completed_files"`
	Failure             string             `json:"failure,omitempty"`
	Current             string             `json:"current_step,omitempty"`
	CurrentItem         string             `json:"current_item,omitempty"`
}

// Report retains the complete inventory for saved reports and explicit detail views.
func Report(w io.Writer, v *Inventory, j *Journal) error {
	return inventoryReport(w, v, j, true)
}

func CompactReport(w io.Writer, v *Inventory, j *Journal) error {
	return inventoryReport(w, v, j, false)
}

func inventoryReport(w io.Writer, v *Inventory, j *Journal, detailed bool) error {
	paint := reportColors(w)
	var b strings.Builder
	status := "Preview (read-only)"
	if j != nil {
		status = j.Phase
		if j.Failure != "" {
			status = "staging incomplete"
			if j.Merge != nil {
				status = "merge incomplete"
			}
		}
		if j.Phase == "completed" && j.Merge != nil && len(j.Merge.Plan.Excluded) > 0 {
			status = "completed with exclusions"
		}
	}
	stagingLabel := "Staging"
	if j == nil || j.ID == "" {
		stagingLabel = "Planned staging path"
	}
	fmt.Fprintf(&b, "%s\nStatus: %s\nSource: %s\nDestination: %s\n%s: %s\n", paint.heading("Devbox -> Neo migration"), display(status), display(v.Paths.Source), display(v.Paths.Destination), stagingLabel, display(v.Paths.Work))
	if j != nil && j.ID != "" {
		if detailed {
			fmt.Fprintf(&b, "Run: %s\nStarted: %s\nUpdated: %s\n", j.ID, j.Started.Format(time.RFC3339), j.Updated.Format(time.RFC3339))
		}
		fmt.Fprintf(&b, "Recorded staged files: %d\n", len(j.Completed))
		if j.Failure != "" {
			fmt.Fprintf(&b, "Error: %s\n", display(j.Failure))
		}
	}
	if j == nil || j.ID == "" {
		if detailed {
			fmt.Fprintln(&b, "Metadata only: [Metadata OK] means no metadata errors found, not a validated import. No data has been copied. Payload files/links, sizes, deep-tree checks and hashing are deferred until staging selection. Docker checks and final configuration review are still required.")
		} else {
			fmt.Fprintln(&b, "\nMetadata only — not a validated import. No data has been copied.")
			fmt.Fprintln(&b, "Payload/link checks happen during staging; Docker and final config checks follow.")
		}
	}
	var notes reportNotes
	group := ""
	if !detailed {
		notes = collectReportNotes(v.Items)
		fmt.Fprintln(&b, "\n"+inventorySummary(v.Items, j))
	}
	for _, i := range v.Items {
		state := reportItemState(i, j)
		if !detailed {
			if group != i.Kind {
				group = i.Kind
				fmt.Fprintf(&b, "\n%s\n", paint.heading(reportGroup(group)))
			} else if i.Kind == "session" || i.Kind == "profile" {
				fmt.Fprintln(&b)
			}
			compactItem(&b, i, j, state, notes, paint)
			continue
		}
		fmt.Fprintf(&b, "\n%s %s\n  Path: %s\n", paint.status(state), paint.item(i), display(i.Path))
		if i.Harness != "" {
			fmt.Fprintf(&b, "  Harness: %s\n", display(i.Harness))
		}
		if i.Workspace != "" {
			fmt.Fprintf(&b, "  Workspace: %s\n  Profile slot: %s\n  Source profile: %s\n  Session ID: %s\n  Proposed target: %s\n  Created: %s\n  Last activity: %s\n", display(i.Workspace), display(i.Profile), display(i.SourceProfile), display(i.SessionID), display(i.Target), display(i.Created), display(i.Activity))
		}
		if i.Action != "" {
			fmt.Fprintf(&b, "  Last action: %s\n", display(i.Action))
		}
		if i.Alias != "" {
			fmt.Fprintf(&b, "  Old alias (report only): %s\n", display(i.Alias))
		}
		if i.ClonedFrom != "" {
			fmt.Fprintf(&b, "  Cloned from (report only): %s\n", display(i.ClonedFrom))
		}
		for _, from := range i.RelocatedFrom {
			fmt.Fprintf(&b, "  Relocated from (report only): %s\n", display(from))
		}
		if j != nil && j.Excluded[i.Key] != "" {
			fmt.Fprintf(&b, "  Exclusion: %s\n", display(j.Excluded[i.Key]))
		}
		for _, issue := range i.Issues {
			fmt.Fprintf(&b, "  Error: %s\n", display(issue))
		}
		for _, notice := range i.Notices {
			fmt.Fprintf(&b, "  Review: %s\n", display(notice))
		}
		for _, change := range i.Changes {
			fmt.Fprintf(&b, "  Conversion: %s\n", display(change))
		}
		for _, warning := range i.Warnings {
			fmt.Fprintf(&b, "  Warning: %s\n", display(warning))
		}
		if i.Scanned {
			fmt.Fprintf(&b, "  Portable bytes: %d\n", i.Bytes)
		} else {
			fmt.Fprintln(&b, "  Portable data: not scanned; only selected data is scanned during staging.")
		}
	}
	if !detailed && len(notes.shared) > 0 {
		fmt.Fprintln(&b, "\n"+paint.heading("Shared notes summary:"))
		for n, note := range notes.shared {
			fmt.Fprintf(&b, "  %d. %s\n", n+1, display(note))
		}
	}
	if !detailed {
		fmt.Fprintln(&b, "\nFull inventory details: --verbose (also kept in the saved report).")
	}
	fmt.Fprintln(&b, "\n"+paint.heading("Retained originals and limitations:"))
	for _, notice := range v.Notices {
		fmt.Fprintf(&b, "  %s\n", display(notice))
	}
	if j != nil && j.Merge != nil {
		if err := mergeProgress(&b, j); err != nil {
			return err
		}
		_, err := io.WriteString(w, b.String())
		return err
	}
	fmt.Fprintln(&b, "\nNo sessions have been imported into Neo. Explicit merge approval is required.")
	if j != nil {
		for key, c := range j.Captures {
			fmt.Fprintf(&b, "Captured container config: %s -> %s\n", display(key), display(c.Root))
		}
	}
	if j != nil && j.ID != "" {
		fmt.Fprintf(&b, "\nReport: %s\n", display(filepath.Join(v.Paths.Work, "report.txt")))
		if j.Phase == "prepared" {
			fmt.Fprintf(&b, "Review merge:\n  devbox-migrate --source %s --destination %s --merge\n", shell(v.Paths.Source), shell(v.Paths.Destination))
		} else {
			fmt.Fprintf(&b, "Resume:\n  devbox-migrate --source %s --destination %s --resume\n", shell(v.Paths.Source), shell(v.Paths.Destination))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
func display(s string) string {
	for _, r := range s {
		if r < 32 || r == 127 {
			return strconv.QuoteToASCII(s)
		}
	}
	return s
}
func shell(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func saveJournal(j *Journal) error {
	j.Updated = time.Now().UTC()
	if err := fsutil.JSON(filepath.Join(j.Inventory.Paths.Work, "journal.json"), j); err != nil {
		return err
	}
	var b strings.Builder
	if err := Report(&b, &j.Inventory, j); err != nil {
		return err
	}
	return fsutil.Write(filepath.Join(j.Inventory.Paths.Work, "report.txt"), []byte(b.String()), 0600)
}
func Load(p Paths) (*Journal, error) {
	checked, err := NewPaths(p.Source, p.Destination)
	if err != nil {
		return nil, err
	}
	if checked != p {
		return nil, fmt.Errorf("invalid migration paths")
	}
	b, err := readRegular(context.Background(), filepath.Join(p.Work, "journal.json"))
	if err != nil {
		return nil, err
	}
	var j Journal
	if config.Decode(b, &j) != nil || j.Version != journalVersion || !idPattern.MatchString(j.ID) || j.Inventory.Paths != p || (j.Inventory.Installation != "" && !idPattern.MatchString(j.Inventory.Installation)) {
		return nil, fmt.Errorf("unrecognized or mismatched migration journal; no files changed")
	}
	if j.Inventory.SourceHashes == nil || j.Inventory.Directories == nil || j.Excluded == nil || j.Completed == nil {
		return nil, fmt.Errorf("incomplete migration journal")
	}
	if j.Phase != "staging" && j.Phase != "prepared" && j.Phase != "merging" && j.Phase != "completed" {
		return nil, fmt.Errorf("unsupported migration journal phase")
	}
	if (j.Phase == "merging" || j.Phase == "completed") && (j.Merge == nil || !idPattern.MatchString(j.Merge.Plan.Installation) || j.Merge.Published == nil || j.Merge.Attempts == nil) {
		return nil, fmt.Errorf("incomplete merge journal")
	}
	if err := validateMerge(&j); err != nil {
		return nil, err
	}
	return &j, nil
}
func Stage(ctx context.Context, v *Inventory, s Selection, runtime SourceRuntime, out io.Writer) (*Journal, error) {
	excluded, err := v.Select(s)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(v.Paths.Work); err == nil {
		return nil, fmt.Errorf("migration work directory already exists; review its journal and use --resume")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if runtime == nil {
		return nil, fmt.Errorf("source Docker inspection is required for staging")
	}
	release, err := lockSource(ctx, v)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = checkLeases(ctx, v); err != nil {
		return nil, err
	}
	if err = checkRuntime(ctx, runtime, v, excluded); err != nil {
		return nil, err
	}
	if err = verifySource(ctx, v); err != nil {
		return nil, err
	}
	fresh, err := InventorySource(ctx, v.Paths)
	if err != nil {
		return nil, err
	}
	if digest(encode(fresh)) != digest(encode(v)) {
		return nil, fmt.Errorf("source inventory changed; rescan before staging")
	}
	v, err = snapshotSelected(ctx, fresh, excluded, newPreparationProgress(out))
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(v.Paths.Work, 0700); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(v.Paths.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	id, err := fsutil.ID()
	if err != nil {
		return nil, err
	}
	j := &Journal{Version: journalVersion, ID: id, Started: time.Now().UTC(), Phase: "staging", Inventory: *v, Selection: s, Excluded: excluded, Completed: map[string]string{}}
	if err = saveJournal(j); err != nil {
		return j, err
	}
	return j, stageFiles(ctx, j, v, runtime)
}
func ResumeStage(ctx context.Context, p Paths, runtime SourceRuntime, out io.Writer) (result *Journal, err error) {
	original, err := Load(p)
	if err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(p.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	j, err := Load(p)
	if err != nil {
		return nil, err
	}
	if j.ID != original.ID {
		return nil, fmt.Errorf("migration run changed while acquiring its lock")
	}
	if j.Merge != nil {
		return j, fmt.Errorf("this run requires merge resume")
	}
	if j.Phase == "prepared" {
		j.Failure = ""
		if err := saveJournal(j); err != nil {
			return j, err
		}
		return j, fmt.Errorf("import is already prepared; --resume does not authorize merge; use --merge for explicit review")
	}
	copying := false
	defer func() {
		if err != nil && !copying {
			j.Current = "checking staged-run inputs"
			j.CurrentItem = ""
			j.Failure = err.Error()
			err = errors.Join(err, saveJournal(j))
		}
	}()
	v, err := InventorySource(ctx, p)
	if err != nil {
		return j, err
	}
	excluded, err := v.Select(j.Selection)
	if err != nil {
		return j, err
	}
	if digest(encode(excluded)) != digest(encode(j.Excluded)) {
		return j, fmt.Errorf("journal exclusions disagree with its approved scope")
	}
	release, err := lockSource(ctx, v)
	if err != nil {
		return j, err
	}
	defer release()
	if runtime == nil {
		return j, fmt.Errorf("source Docker inspection is required")
	}
	if err = checkLeases(ctx, v); err != nil {
		return j, err
	}
	if err = checkRuntime(ctx, runtime, v, j.Excluded); err != nil {
		return j, err
	}
	if err = verifySource(ctx, v); err != nil {
		return j, err
	}
	v, err = snapshotSelected(ctx, v, excluded, newPreparationProgress(out))
	if err != nil {
		return j, err
	}
	if digest(encode(v)) != digest(encode(&j.Inventory)) {
		return j, fmt.Errorf("source snapshot changed; review/restage before continuing")
	}
	copying = true
	return j, stageFiles(ctx, j, v, runtime)
}
func verifySource(ctx context.Context, v *Inventory) error {
	keys := make([]string, 0, len(v.SourceHashes))
	for path := range v.SourceHashes {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for root, expected := range v.Directories {
		actual, err := v.directoryNames(root)
		if err != nil || digest(encode(actual)) != digest(encode(expected)) {
			return fmt.Errorf("source directory changed; review/restage: %s", root)
		}
	}
	for _, file := range v.Files {
		if file.Data != nil || !file.Mode.IsRegular() {
			continue
		}
		info, err := os.Lstat(file.Source)
		if err != nil || !info.Mode().IsRegular() || privateMode(info.Mode()) != file.Mode {
			return fmt.Errorf("source type/mode changed; review/restage: %s", file.Source)
		}
	}
	for _, path := range keys {
		expected := v.SourceHashes[path]
		if _, err := fsutil.Path(filepath.Dir(path), "."); err != nil {
			return err
		}
		if expected == "missing" {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return fmt.Errorf("source presence changed; review/restage: %s", path)
			}
			continue
		}
		if strings.HasPrefix(expected, "link:") {
			link, err := os.Readlink(path)
			if err != nil || "link:"+digest([]byte(link)) != expected {
				return fmt.Errorf("source link changed; review/restage: %s", path)
			}
			continue
		}
		if _, err := fsutil.Path(filepath.Dir(path), filepath.Base(path)); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("source is no longer a regular file: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("source unavailable: %s", path)
		}
		v.progress.file(path)
		h := sha256.New()
		_, err = io.Copy(h, v.progress.reader(ctx, f))
		f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != expected {
			return fmt.Errorf("source changed; review/restage: %s", path)
		}
	}
	return nil
}
func stageFiles(ctx context.Context, j *Journal, v *Inventory, runtime SourceRuntime) (err error) {
	defer func() {
		if err != nil {
			j.Failure = fmt.Sprintf("Could not finish %s: %s Source, destination, and project data were not changed.", j.Current, err.Error())
			err = errors.Join(err, saveJournal(j))
		}
	}()
	j.Failure = ""
	j.Current = "preparing staging directories"
	j.CurrentItem = ""
	root, err := fsutil.Dir(v.Paths.Work, "staged-home", 0700)
	if err != nil {
		return err
	}
	v.progress.begin("Copying selected data")
	for _, file := range v.Files {
		if j.Excluded[file.Item] != "" {
			continue
		}
		j.Current = "copying " + file.Relative
		j.CurrentItem = file.Item
		if !filepath.IsLocal(file.Relative) {
			return fmt.Errorf("invalid staged destination")
		}
		target := filepath.Join(root, file.Relative)
		if _, err := fsutil.Path(filepath.Dir(target), "."); err != nil {
			return err
		}
		if file.Mode.IsDir() {
			if _, err = fsutil.Dir(root, file.Relative, 0700); err != nil {
				return err
			}
			continue
		}
		v.progress.file(file.Relative)
		if _, err = fsutil.Dir(filepath.Dir(target), ".", 0700); err != nil {
			return err
		}
		// A crash after no-replace publication is recognized by the expected bytes.
		// An unrelated file is never overwritten, even inside a recognized run.
		if _, e := os.Lstat(target); e == nil {
			match, e := matches(ctx, target, file)
			if e != nil || !match {
				return fmt.Errorf("staged file changed or conflicts: %s", target)
			}
		} else if !os.IsNotExist(e) {
			return e
		} else if err = copyFile(ctx, target, file, v.progress); err != nil {
			return err
		}
		j.Completed[file.Relative] = file.Hash
		if len(j.Completed)%64 == 0 {
			if err = saveJournal(j); err != nil {
				return err
			}
		}
	}
	v.progress.finish()
	v.progress.begin("Verifying selected source snapshot")
	j.Current = "verifying the source snapshot"
	j.CurrentItem = ""
	if err = checkLeases(ctx, v); err != nil {
		return err
	}
	if err = checkRuntime(ctx, runtime, v, j.Excluded); err != nil {
		return err
	}
	if err = verifySource(ctx, v); err != nil {
		return err
	}
	v.progress.finish()
	if err = syncStagedDirectories(ctx, root); err != nil {
		return err
	}
	j.Phase = "prepared"
	j.Failure = ""
	j.Current = "host-backed staging verified"
	j.CurrentItem = ""
	return saveJournal(j)
}

// Empty directories and intermediate store ancestors must be durable before
// the journal can describe the staged tree as complete.
func syncStagedDirectories(ctx context.Context, root string) error {
	dirs := []string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	for n := len(dirs) - 1; n >= 0; n-- {
		f, err := os.Open(dirs[n])
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	return nil
}
func matches(ctx context.Context, path string, file File) (bool, error) {
	if file.Mode&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		return link == file.Link, err
	}
	if _, err := fsutil.Path(filepath.Dir(path), filepath.Base(path)); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != file.Mode.Perm() {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, contextReader{ctx, f}); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == file.Hash, nil
}
func copyFile(ctx context.Context, target string, file File, progress *preparationProgress) error {
	if file.Mode&os.ModeSymlink != 0 {
		if err := os.Symlink(file.Link, target); err != nil {
			return err
		}
		d, err := os.Open(filepath.Dir(target))
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
	if file.Data != nil {
		if digest(file.Data) != file.Hash {
			return fmt.Errorf("generated file does not match reviewed copy plan")
		}
		return fsutil.WriteNew(target, file.Data, file.Mode)
	}
	if _, err := fsutil.Path(filepath.Dir(file.Source), filepath.Base(file.Source)); err != nil {
		return err
	}
	info, err := os.Lstat(file.Source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || privateMode(info.Mode()) != file.Mode {
		return fmt.Errorf("source type/mode changed: %s", file.Source)
	}
	in, err := os.Open(file.Source)
	if err != nil {
		return fmt.Errorf("cannot read copy source: %s", file.Source)
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), ".copy-*")
	if err != nil {
		return err
	}
	name := out.Name()
	defer os.Remove(name)
	defer out.Close()
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(out, h), progress.reader(ctx, in)); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != file.Hash {
		return fmt.Errorf("source changed during copy: %s", file.Source)
	}
	if err = out.Chmod(file.Mode); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	// Streamed histories can be much larger than memory. Publish the synced
	// temporary file without replacing any existing destination entry.
	if err = unix.Renameat2(unix.AT_FDCWD, name, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "publish without replacement", Old: name, New: target, Err: err}
	}
	d, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
