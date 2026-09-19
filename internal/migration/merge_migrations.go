package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/filesync"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

// MergeChoices are explicit user decisions, not instructions to overwrite a
// whole home. Skip keys retain the same meaning as the staging inventory.
type MergeChoices struct {
	Skip        []string          `json:"skip,omitempty"`
	Rename      map[string]string `json:"rename,omitempty"`
	Reuse       []string          `json:"reuse,omitempty"`
	Projects    []string          `json:"projects,omitempty"`
	Global      string            `json:"global,omitempty"`
	ReplaceAuth []string          `json:"replace_auth,omitempty"`
	Accept      []string          `json:"accept,omitempty"`
	OmitConfig  []string          `json:"omit_container_config,omitempty"`
}
type Review struct {
	Key, Item, Message string
	Blocking           bool
}
type Publication struct {
	Key          string `json:"key"`
	Item         string `json:"item"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	Before       string `json:"before"`
	After        string `json:"after"`
	Directory    bool   `json:"directory"`
	Backup       string `json:"backup,omitempty"`
	BackupHash   string `json:"backup_hash,omitempty"`
	OriginalMode uint32 `json:"original_mode,omitempty"`
}
type ImportSession struct {
	Item     string                   `json:"item"`
	Identity environment.Identity     `json:"identity"`
	ID       string                   `json:"id"`
	Created  time.Time                `json:"created"`
	Activity time.Time                `json:"activity"`
	Action   string                   `json:"action"`
	Desired  environment.Fingerprints `json:"desired"`
}
type MergePlan struct {
	Captures            map[string]Capture `json:"captures,omitempty"`
	Installation        string             `json:"installation"`
	Fresh               bool               `json:"fresh"`
	Choices             MergeChoices       `json:"choices"`
	Excluded            map[string]string  `json:"excluded"`
	Publications        []Publication      `json:"publications"`
	Sessions            []ImportSession    `json:"sessions"`
	Reviews             []Review           `json:"reviews"`
	Notices             []string           `json:"notices"`
	Warnings            []string           `json:"warnings,omitempty"`
	DestinationSnapshot string             `json:"destination_snapshot"`
}

func (p MergePlan) Ready() error {
	for _, r := range p.Reviews {
		if r.Blocking {
			return fmt.Errorf("%s: %s", r.Item, r.Message)
		}
		if !contains(p.Choices.Accept, r.Key) {
			return fmt.Errorf("review and explicitly accept %s", r.Key)
		}
	}
	return nil
}
func (p *MergePlan) block(key, item, message string) {
	p.Reviews = append(p.Reviews, Review{key, item, message, true})
}
func (p *MergePlan) change(key, item, message string) {
	p.Reviews = append(p.Reviews, Review{key, item, message, false})
}
func (p MergePlan) Fingerprint() string { return digest(encode(p)) }

type Capture struct {
	Root      string   `json:"root"`
	Container string   `json:"container"`
	Hash      string   `json:"hash"`
	Changes   []string `json:"changes,omitempty"`
}
type Attempt struct {
	Phase     string `json:"phase"`
	Temporary string `json:"temporary"`
	Device    uint64 `json:"device"`
	Inode     uint64 `json:"inode"`
}
type MergeState struct {
	Plan      MergePlan           `json:"plan"`
	Approved  time.Time           `json:"approved"`
	Published map[string]bool     `json:"published"`
	Intent    map[string]bool     `json:"publication_intent"`
	Attempts  map[string]*Attempt `json:"attempts"`
	Bootstrap string              `json:"bootstrap,omitempty"`
}

// Merger carries current-runtime dependencies. Tests use the ordinary fake
// Docker daemon and temporary homes, never host installations.
type Merger struct {
	Docker   docker.Runtime
	Host     config.Host
	UID, GID int
	Progress io.Writer
	Fault    func(string) error
}

func (m Merger) fault(point string) error {
	if m.Fault != nil {
		return m.Fault(point)
	}
	return nil
}
func (m Merger) host() config.Host {
	if m.Host != nil {
		return m.Host
	}
	return config.Snapshot()
}
func (m Merger) note(text string) {
	if m.Progress != nil {
		fmt.Fprintln(m.Progress, text)
	}
}
func publicFailure(message, target string, cause error) error {
	return commanderror.New("migration_failed", message, target, cause)
}

func stateHash(ctx context.Context, path string) (string, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "missing", nil
	} else if err != nil {
		return "", err
	}
	var entries []File
	err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		f := File{Relative: rel, Mode: info.Mode()}
		if e.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			f.Link = link
		} else if info.Mode().IsRegular() {
			if _, err := fsutil.Path(filepath.Dir(p), filepath.Base(p)); err != nil {
				return err
			}
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, contextReader{ctx, in})
			in.Close()
			if err != nil {
				return err
			}
			f.Hash = hex.EncodeToString(h.Sum(nil))
		} else if !info.IsDir() {
			return fmt.Errorf("unsupported filesystem entry: %s", p)
		}
		entries = append(entries, f)
		return nil
	})
	return digest(encode(entries)), err
}

// copyTree is used only for private previews and no-replace publication. Links
// must remain internal under relocation; it never follows arbitrary host links.
func copyTree(ctx context.Context, source, target string) error {
	if _, err := fsutil.Path(source, "."); err != nil {
		return err
	}
	modes := map[string]os.FileMode{}
	err := filepath.WalkDir(source, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		to := filepath.Join(target, rel)
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			modes[to] = info.Mode().Perm()
			_, err = fsutil.Dir(to, ".", 0700)
			return err
		}
		if _, err = fsutil.Dir(filepath.Dir(to), ".", 0700); err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil || filepath.IsAbs(link) || !filepath.IsLocal(filepath.Join(filepath.Dir(rel), link)) || !within(source, resolved) {
				return fmt.Errorf("unmapped symlink: %s", p)
			}
			return os.Symlink(link, to)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported copy entry: %s", p)
		}
		// Configuration/build trees are already read into memory by the resolver.
		b, err := readRegular(ctx, p)
		if err != nil {
			return err
		}
		return fsutil.WriteNew(to, b, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	paths := []string{}
	for path := range modes {
		paths = append(paths, path)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, path := range paths {
		if err = os.Chmod(path, modes[path]); err != nil {
			return err
		}
	}
	return nil
}
func removePreview(path string) {
	_ = filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return os.Chmod(p, 0700)
		}
		return nil
	})
	_ = os.RemoveAll(path)
}
func verifyStaging(ctx context.Context, j *Journal) error {
	root := filepath.Join(j.Inventory.Paths.Work, "staged-home")
	known := map[string]bool{".": true}
	for _, f := range j.Inventory.Files {
		if j.Excluded[f.Item] != "" {
			continue
		}
		if !filepath.IsLocal(f.Relative) {
			return fmt.Errorf("invalid staged manifest path")
		}
		for p := f.Relative; p != "."; p = filepath.Dir(p) {
			known[p] = true
		}
		path := filepath.Join(root, f.Relative)
		if f.Mode.IsDir() {
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("staged directory missing: %s", path)
			}
			continue
		}
		ok, err := matches(ctx, path, f)
		if err != nil || !ok {
			return fmt.Errorf("staged data changed: %s", path)
		}
	}
	return filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if !known[rel] {
			return fmt.Errorf("unreviewed staged entry: %s", path)
		}
		return ctx.Err()
	})
}
func destinationInventory(ctx context.Context, home string) (string, []store.Record, string, error) {
	if _, err := fsutil.Path(home, "."); err != nil {
		return "", nil, "", err
	}
	if _, err := os.Lstat(home); os.IsNotExist(err) {
		return "", nil, "missing", nil
	} else if err != nil {
		return "", nil, "", err
	}
	b, err := readRegular(ctx, filepath.Join(home, "state/installation-id"))
	if err != nil {
		return "", nil, "", fmt.Errorf("destination is not an identified Neo home; do not initialize over it")
	}
	id := strings.TrimSpace(string(b))
	if !idPattern.MatchString(id) {
		return "", nil, "", fmt.Errorf("invalid destination installation identity")
	}
	records := []store.Record{}
	entries, err := readEntries(filepath.Join(home, "sessions"))
	if err != nil {
		return "", nil, "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			return "", nil, "", fmt.Errorf("unrecognized destination session entry")
		}
		b, err := readRegular(ctx, filepath.Join(home, "sessions", e.Name(), "session.json"))
		if err != nil {
			return "", nil, "", fmt.Errorf("destination has incomplete session state; review it before merge")
		}
		var r store.Record
		if config.Decode(b, &r) != nil || r.Validate(e.Name()) != nil {
			return "", nil, "", fmt.Errorf("destination has invalid session state; review it before merge")
		}
		records = append(records, r)
	}
	transfers, err := (&store.Store{Home: home, Installation: id}).Transfers()
	if err != nil || len(transfers) > 0 {
		return "", nil, "", fmt.Errorf("destination has pending or invalid transfers")
	}
	facts := map[string]string{"installation": id}
	for _, part := range []string{"config.json", "profiles", "harnesses", "auth"} {
		h, err := stateHash(ctx, filepath.Join(home, part))
		if err != nil {
			return "", nil, "", err
		}
		facts[part] = h
	}
	for _, r := range records {
		facts["session:"+r.Identity.Name] = digest(encode(r))
	}
	return id, records, digest(encode(facts)), nil
}
func excludedMerge(j *Journal, c MergeChoices) (map[string]string, error) {
	s := j.Selection
	s.Skip = append(append([]string(nil), s.Skip...), c.Skip...)
	excluded, err := j.Inventory.Select(s)
	if err != nil {
		return nil, err
	}
	for k, v := range j.Excluded {
		excluded[k] = v
	}
	return excluded, nil
}
func stagedItemRoot(j *Journal, i Item) string {
	rel := ""
	switch i.Kind {
	case "profile":
		rel = filepath.Join("profiles", i.Name)
	case "project":
		rel = filepath.Join("projects", digest([]byte(i.Name)))
	case "auth":
		rel = filepath.Join("auth", i.Harness, "auth.json")
	case "cache":
		rel = filepath.Join("cache/harnesses", i.Name)
	case "global":
		rel = "config.json"
	}
	return filepath.Join(j.Inventory.Paths.Work, "staged-home", rel)
}
func (m Merger) Plan(ctx context.Context, j *Journal, c MergeChoices) (plan MergePlan, err error) {
	if j.Phase != "prepared" || j.Merge != nil {
		return plan, fmt.Errorf("only a prepared, unmerged run can be reviewed; resume an approved merge instead")
	}
	if err = verifyStaging(ctx, j); err != nil {
		return plan, err
	}
	if err = validateChoices(j, c); err != nil {
		return plan, err
	}
	if c.Global != "" && c.Global != "keep" && c.Global != "import" {
		return plan, fmt.Errorf("global choice must be keep or import")
	}
	plan = MergePlan{Choices: c, Excluded: map[string]string{}, Captures: map[string]Capture{}}
	plan.Excluded, err = excludedMerge(j, c)
	if err != nil {
		return plan, err
	}
	home := j.Inventory.Paths.Destination
	id, records, snapshot, err := destinationInventory(ctx, home)
	if err != nil {
		return plan, err
	}
	plan.DestinationSnapshot = snapshot
	plan.Fresh = id == ""
	if id == "" {
		id = j.PlannedInstallation
		if id == "" {
			id, err = fsutil.ID()
			if err != nil {
				return plan, err
			}
		}
	}
	plan.Installation = id
	if j.PlannedInstallation != id {
		j.PlannedInstallation = id
		if err = saveJournal(j); err != nil {
			return plan, err
		}
	}
	preview, err := os.MkdirTemp(j.Inventory.Paths.Work, "preview-")
	if err != nil {
		return plan, err
	}
	defer removePreview(preview)
	if _, err = fsutil.Dir(preview, "profiles", 0700); err != nil {
		return plan, err
	}
	if _, err = fsutil.Dir(preview, "harnesses", 0700); err != nil {
		return plan, err
	}
	for _, name := range []string{"pi", "opencode"} {
		source := filepath.Join(home, "harnesses", name)
		if _, e := os.Lstat(source); e == nil {
			if err = copyTree(ctx, source, filepath.Join(preview, "harnesses", name)); err != nil {
				return plan, err
			}
		} else if !os.IsNotExist(e) {
			return plan, e
		}
	}
	addPublication := func(item, source, target string, directory bool) error {
		before, e := stateHash(ctx, target)
		if e != nil {
			return e
		}
		after, e := stateHash(ctx, source)
		if e != nil {
			return e
		}
		if after == "missing" {
			return fmt.Errorf("publication source is missing")
		}
		if before == after {
			plan.Notices = append(plan.Notices, "Already identical: "+target)
			return nil
		}
		key := digest([]byte(target))
		backup := ""
		if before != "missing" {
			backup = filepath.Join(j.Inventory.Paths.Work, "destination-backups", key)
		}
		if strings.HasPrefix(item, "project:") {
			backup = filepath.Join(j.Inventory.Paths.Work, "project-backups", key)
		}
		backupHash := ""
		var mode uint32
		if before != "missing" && !directory {
			b, e := readRegular(ctx, target)
			if e != nil {
				return e
			}
			backupHash = digest(b)
			info, e := os.Stat(target)
			if e != nil {
				return e
			}
			mode = uint32(info.Mode().Perm())
		}
		if before == "missing" {
			backup = ""
		}
		plan.Publications = append(plan.Publications, Publication{Key: key, Item: item, Source: source, Target: target, Before: before, After: after, Directory: directory, Backup: backup, BackupHash: backupHash, OriginalMode: mode})
		return nil
	}
	globalSource := filepath.Join(home, "config.json")
	globalItem := j.Inventory.item("global:config")
	importGlobal := plan.Fresh || c.Global == "import"
	if plan.Fresh && c.Global == "keep" {
		plan.block("global", "global:config", "There are no destination globals to keep; choose import.")
		return plan, nil
	}
	if importGlobal {
		if globalItem == nil || plan.Excluded[globalItem.Key] != "" {
			plan.block("global", "global:config", "No staged global configuration selected for this destination.")
			return plan, nil
		}
		globalSource = stagedItemRoot(j, *globalItem)
	}
	b, err := readRegular(ctx, globalSource)
	if err != nil {
		return plan, err
	}
	g, err := config.ParseGlobal(b)
	if err != nil {
		return plan, publicFailure("Cannot parse selected global configuration.", globalSource, err)
	}
	if importGlobal && c.Rename[g.DefaultProfile] != "" {
		g.DefaultProfile = c.Rename[g.DefaultProfile]
		b = encode(g)
	}
	if err = fsutil.WriteNew(filepath.Join(preview, "config.json"), b, 0600); err != nil {
		return plan, err
	}
	if importGlobal {
		input, err := saveMergeInput(j, b)
		if err != nil {
			return plan, err
		}
		if err = addPublication("global:config", input, filepath.Join(home, "config.json"), false); err != nil {
			return plan, err
		}
		if !plan.Fresh {
			plan.change("global:replace", "global:config", "Replace Neo global settings; this affects existing environments as well as imports.")
		}
	} else {
		plan.Notices = append(plan.Notices, "Keep existing Neo global defaults and environment settings.")
	}
	profiles := map[string]string{}
	for _, i := range j.Inventory.Items {
		if i.Kind != "profile" || plan.Excluded[i.Key] != "" {
			continue
		}
		name := i.Name
		if c.Rename[name] != "" {
			name = c.Rename[name]
		}
		if !config.Name.MatchString(name) {
			plan.block(i.Key, i.Key, "Choose a valid Neo profile name.")
			continue
		}
		source := stagedItemRoot(j, i)
		target := filepath.Join(home, "profiles", name)
		if _, exists := profiles[name]; exists {
			plan.block(i.Key, i.Key, "Two imports select the same profile name.")
			continue
		}
		if _, e := os.Lstat(target); e == nil {
			if !contains(c.Reuse, i.Key) {
				plan.block(i.Key, i.Key, "Profile already exists: explicitly reuse it, rename the import, or skip it.")
				continue
			}
			source = target
			plan.Notices = append(plan.Notices, "Reuse existing profile: "+name)
		} else if !os.IsNotExist(e) {
			return plan, e
		} else if contains(c.Reuse, i.Key) {
			plan.block(i.Key, i.Key, "No destination profile exists to reuse.")
			continue
		} else if err = addPublication(i.Key, source, target, true); err != nil {
			return plan, err
		}
		profiles[name] = source
	}
	// Existing defaults may be needed even when they were not source profiles.
	if g.DefaultProfile != "" {
		if _, ok := profiles[g.DefaultProfile]; !ok {
			profiles[g.DefaultProfile] = filepath.Join(home, "profiles", g.DefaultProfile)
		}
	}
	for name, source := range profiles {
		if !config.Name.MatchString(name) {
			plan.block("global", "global:config", "Invalid participating default profile.")
			continue
		}
		if err = copyTree(ctx, source, filepath.Join(preview, "profiles", name)); err != nil {
			plan.block("profile:"+name, "profile:"+name, "Cannot read participating profile; resolve its conflict or missing configuration.")
		}
	}
	for _, i := range j.Inventory.Items {
		if plan.Excluded[i.Key] != "" {
			continue
		}
		switch i.Kind {
		case "auth":
			source := stagedItemRoot(j, i)
			if _, e := os.Lstat(source); os.IsNotExist(e) {
				continue
			}
			target := filepath.Join(home, "auth", i.Harness, "auth.json")
			if _, e := os.Lstat(target); e == nil && !contains(c.ReplaceAuth, i.Key) {
				plan.Notices = append(plan.Notices, "Keep existing authentication: "+target)
				continue
			}
			if contains(c.ReplaceAuth, i.Key) {
				if _, isSource := j.Inventory.SourceHashes[target]; isSource {
					plan.block(i.Key, i.Key, "Cannot replace credentials that are also a source auth input.")
					continue
				}
				plan.change("replace:"+i.Key, i.Key, "Replace authentication used by existing and imported environments; keep a backup.")
			}
			if err = addPublication(i.Key, source, target, false); err != nil {
				return plan, err
			}
		case "cache":
			source := stagedItemRoot(j, i)
			target := filepath.Join(home, "cache/harnesses", i.Name)
			if _, e := os.Lstat(target); e == nil {
				plan.Notices = append(plan.Notices, "Keep existing cache: "+target)
				continue
			}
			if err = addPublication(i.Key, source, target, true); err != nil {
				return plan, err
			}
		case "project":
			source := filepath.Join(stagedItemRoot(j, i), "config.json")
			target := filepath.Join(i.Path, "config.json")
			before, e := stateHash(ctx, target)
			if e != nil {
				return plan, e
			}
			after, e := stateHash(ctx, source)
			if e != nil {
				return plan, e
			}
			if before == after {
				continue
			}
			if !contains(c.Projects, i.Key) {
				plan.block(i.Key, i.Key, "Approve this exact project config conversion or skip its dependent sessions.")
				continue
			}
			if err = addPublication(i.Key, source, target, false); err != nil {
				return plan, err
			}
			plan.change("edit:"+i.Key, i.Key, "Edit shared project configuration; returning to the old CLI may require restoring its backup.")
		}
	}
	for _, i := range j.Inventory.Items {
		if i.Kind != "session" || plan.Excluded[i.Key] != "" {
			continue
		}
		profile := i.Profile
		if c.Rename[profile] != "" {
			profile = c.Rename[profile]
		}
		if profile == "" {
			profile = i.SourceProfile
			if c.Rename[profile] != "" {
				profile = c.Rename[profile]
			}
		}
		var identity environment.Identity
		var e error
		if i.Profile == "" {
			// A source project session records its base profile (including none).
			// Destination defaults must not silently select a different combination.
			identity, e = environment.Identify(i.Workspace, profile, true)
		} else {
			identity, e = environment.Select(preview, i.Workspace, profile, false, m.host())
		}
		if e != nil {
			plan.block(i.Key, i.Key, "Workspace is unavailable or noncanonical.")
			continue
		}
		collision := false
		for _, r := range records {
			if r.Identity.Name == identity.Name || r.ID == i.SessionID {
				collision = true
			}
		}
		if _, e := os.Lstat(filepath.Join(home, "sessions", identity.Name)); e == nil {
			collision = true
		} else if !os.IsNotExist(e) {
			return plan, e
		}
		if collision {
			plan.block(i.Key, i.Key, "Destination session slot or ID already exists; skip this import. Existing history will not be replaced.")
			continue
		}
		if _, exists, e := m.Docker.Inspect(ctx, identity.Name); e != nil {
			return plan, publicFailure("Cannot inspect destination Docker resources.", identity.Name, e)
		} else if exists {
			plan.block(i.Key, i.Key, "Docker container name is occupied; no adoption or replacement is permitted.")
			continue
		}
		tag := docker.Namespace + "/session:" + i.SessionID
		if exists, e := imageTagExists(ctx, m.Docker, tag); e != nil {
			return plan, e
		} else if exists {
			plan.block(i.Key, i.Key, "Session image tag is occupied; no existing image association will be replaced.")
			continue
		}
		var meta oldMetadata
		data, e := readRegular(ctx, filepath.Join(i.Path, "metadata.json"))
		if e != nil || config.Decode(data, &meta) != nil || meta.Creation == nil {
			plan.block(i.Key, i.Key, "Source creation settings are unavailable.")
			continue
		}
		var proposed *config.Layer
		project := j.Inventory.item("project:" + i.Workspace)
		if project != nil && plan.Excluded[project.Key] == "" {
			data, e := readRegular(ctx, filepath.Join(stagedItemRoot(j, *project), "config.json"))
			if e != nil {
				return plan, e
			}
			l, e := config.ParseLayer(data, true)
			if e != nil {
				return plan, e
			}
			proposed = &l
		}
		q := environment.Request{Home: preview, Workspace: i.Workspace, Profile: profile, Salt: id, UID: m.UID, GID: m.GID, Host: m.host(), Recorded: &identity}
		spec, e := environment.Preview(q, proposed)
		if e != nil {
			plan.block("config:"+i.Key, i.Key, "Final configuration cannot be resolved; review participating profiles/projects and required host environment variables.")
			continue
		}
		if len(spec.Warnings) > 0 {
			plan.change("warnings:"+i.Key, i.Key, strings.Join(spec.Warnings, "; "))
		}
		if spec.Harness.Definition.Name != i.Harness {
			plan.block("harness:"+i.Key, i.Key, "Final configuration would switch the recorded harness; fix configuration or skip, rather than converting conversations.")
			continue
		}
		if e = checkMapping(preview, spec.Harness); e != nil {
			plan.block("mapping:"+i.Key, i.Key, e.Error())
			continue
		}
		mappingOK := true
		for _, name := range []string{"pi", "opencode"} {
			present := false
			for _, f := range j.Inventory.Files {
				if f.Item == i.Key && strings.Contains(filepath.ToSlash(f.Relative), "/harnesses/"+name+"/stores/") {
					present = true
					break
				}
			}
			if present {
				h, e := harness.Load(preview, name)
				if e == nil {
					e = checkMapping(preview, h)
				}
				if e != nil {
					plan.block("retained-mapping:"+i.Key, i.Key, "A retained harness has an untested destination mapping.")
					mappingOK = false
				}
			}
		}
		if !mappingOK {
			continue
		}
		if spec.Harness.Origin != "builtin" {
			plan.change("definition:"+i.Key, i.Key, "Use the destination's user-defined harness installation/launch settings with the verified builtin-shaped state mapping.")
		}
		unsafe := false
		for _, arg := range spec.Settings.DockerArgs {
			if strings.HasPrefix(arg, "--env=") {
				unsafe = true
			}
		}
		if unsafe {
			plan.block("env:"+i.Key, i.Key, "Move raw Docker --env entries into env before importing; raw values must not enter a saved creation record.")
			continue
		}
		fields := changedSettings(meta, spec)
		if len(fields) > 0 {
			plan.change("settings:"+i.Key, i.Key, "Recorded creation settings differ in "+strings.Join(fields, ", ")+". Configure them in the selected sources or explicitly accept the new settings; secret values are withheld.")
		}
		if meta.ProxyEnabled {
			plan.change("proxy:"+i.Key, i.Key, "Proxy protection will not carry over; Neo does not restrict container egress.")
		}
		if identity.Project && identity.Profile != "" {
			plan.change("profile-rules:"+i.Key, i.Key, "The imported session has a separate identity for its profile and project combination.")
		}
		plan.change("create:"+i.Key, i.Key, "Build a fresh environment and run its setup code; shared workspace and external side effects cannot be rolled back.")
		needConfig := false
		for _, f := range j.Inventory.Files {
			if f.Item == i.Key && strings.Contains(filepath.ToSlash(f.Relative), "/harnesses/opencode/stores/data") {
				needConfig = true
			}
		}
		if needConfig {
			if contains(c.OmitConfig, i.Key) {
				plan.change("omit-config:"+i.Key, i.Key, "Container-only OpenCode configuration will not be imported; retain the old container for recovery.")
			} else if captured, ok := j.Captures[i.Key]; !ok {
				plan.block("capture:"+i.Key, i.Key, "Capture the stopped container's OpenCode config, or explicitly omit that unavailable config.")
				continue
			} else {
				h, e := stateHash(ctx, captured.Root)
				if e != nil || h != captured.Hash {
					return plan, fmt.Errorf("captured config changed")
				}
				plan.Captures[i.Key] = captured
				if len(captured.Changes) > 0 {
					plan.change("capture-conversion:"+i.Key, i.Key, strings.Join(captured.Changes, " "))
				}
			}
		}
		managedJournal := *j
		managedJournal.Captures = plan.Captures
		changed, e := previewManaged(ctx, &managedJournal, i, spec, preview)
		if e != nil {
			plan.block("managed:"+i.Key, i.Key, "Imported managed configuration conflicts or contains malformed shared JSON; fix it before import.")
			continue
		}
		if len(changed) > 0 {
			plan.change("managed:"+i.Key, i.Key, "Neo will synchronize imported managed files: "+strings.Join(changed, ", ")+". Preserve durable edits in profile/project sources.")
		}
		created, _ := time.Parse(time.RFC3339Nano, i.Created)
		activity, _ := time.Parse(time.RFC3339Nano, i.Activity)
		plan.Sessions = append(plan.Sessions, ImportSession{Item: i.Key, Identity: identity, ID: i.SessionID, Created: created, Activity: activity, Action: i.Action, Desired: spec.Fingerprints})
	}
	for old, name := range c.Rename {
		if j.Inventory.item("profile:"+old) == nil || !config.Name.MatchString(name) {
			return plan, fmt.Errorf("invalid profile rename decision")
		}
	}
	used := map[string]bool{}
	for _, pub := range plan.Publications {
		used[pub.Item] = true
	}
	for _, job := range plan.Sessions {
		used[job.Item] = true
	}
	for _, item := range j.Inventory.Items {
		if used[item.Key] {
			for _, warning := range item.Warnings {
				plan.Warnings = append(plan.Warnings, item.Key+": "+warning)
			}
			if len(item.Changes) > 0 {
				plan.change("conversion:"+item.Key, item.Key, strings.Join(item.Changes, " "))
			}
		}
	}
	sort.Slice(plan.Publications, func(a, b int) bool { return plan.Publications[a].Target < plan.Publications[b].Target })
	return plan, nil
}
func saveMergeInput(j *Journal, b []byte) (string, error) {
	root, err := fsutil.Dir(j.Inventory.Paths.Work, "merge-inputs", 0700)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, digest(b)+".json")
	if old, err := readRegular(context.Background(), path); err == nil {
		if string(old) != string(b) {
			return "", fmt.Errorf("merge input conflict")
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, fsutil.WriteNew(path, b, 0600)
}
func checkMapping(home string, effective harness.Effective) error {
	// A builtin-shaped custom definition may change installation/launch behavior,
	// but untested store/auth/config layouts cannot be inferred from its name.
	builtin, err := harness.Load(filepath.Join(home, "no-user-definitions"), effective.Definition.Name)
	if err != nil {
		return err
	}
	a, b := effective.Definition, builtin.Definition
	if a.Binary != b.Binary || !reflect.DeepEqual(a.Env, b.Env) || !reflect.DeepEqual(a.Stores, b.Stores) || !reflect.DeepEqual(a.Auth, b.Auth) || a.Config != b.Config || !reflect.DeepEqual(a.Merge, b.Merge) {
		return fmt.Errorf("effective harness definition has an untested state/auth/config mapping")
	}
	return nil
}
func changedSettings(old oldMetadata, s environment.Spec) []string {
	fields := []string{}
	eq := func(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }
	if !eq(old.Creation.Mounts, s.Settings.Mounts) {
		fields = append(fields, "mounts")
	}
	if !eq(old.Creation.Ports, s.Settings.Ports) {
		fields = append(fields, "ports")
	}
	if !eq(old.Creation.DockerArgs, s.Settings.DockerArgs) {
		fields = append(fields, "docker_args")
	}
	if !eq(old.HarnessArgs, s.Settings.HarnessArgs) {
		fields = append(fields, "harness_args")
	}
	network := "default"
	if old.Creation.HostNetwork {
		network = "host"
	}
	if network != s.Settings.Network {
		fields = append(fields, "network")
	}
	env := func(values []string) map[string]string {
		m := map[string]string{}
		for _, v := range values {
			k, x, _ := strings.Cut(v, "=")
			m[k] = x
		}
		return m
	}
	if !reflect.DeepEqual(env(old.Creation.Env), env(s.Env())) {
		fields = append(fields, "environment")
	}
	if old.OnExit != "" {
		fields = append(fields, "removed on_exit")
	}
	if old.Creation.ReadOnly {
		fields = append(fields, "removed workspace read_only")
	}
	return fields
}
func previewManaged(ctx context.Context, j *Journal, i Item, s environment.Spec, preview string) ([]string, error) {
	root, err := os.MkdirTemp(preview, "managed-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	storeRoot := filepath.Join(j.Inventory.Paths.Work, "staged-home/sessions", i.Target, "harnesses", i.Harness, "stores", s.Harness.Definition.Config.Store, s.Harness.Definition.Config.Path)
	if i.Harness == "opencode" {
		if c, ok := j.Captures[i.Key]; ok {
			storeRoot = c.Root
		}
	}
	before := map[string]string{}
	for name := range s.Files {
		path, err := fsutil.Path(storeRoot, name)
		if err != nil {
			return nil, err
		}
		b, err := readRegular(ctx, path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		before[name] = digest(b)
		to := filepath.Join(root, "store", name)
		if _, err = fsutil.Dir(filepath.Dir(to), ".", 0700); err != nil {
			return nil, err
		}
		if err = fsutil.WriteNew(to, b, 0600); err != nil {
			return nil, err
		}
	}
	live, err := fsutil.Dir(root, "store", 0700)
	if err != nil {
		return nil, err
	}
	if err = filesync.Sync(live, filepath.Join(root, "manifest.json"), s.Harness.Definition.Config.Store, s.Files, s.Harness.Definition.Merge); err != nil {
		return nil, err
	}
	changed := []string{}
	for name, hash := range before {
		b, err := readRegular(ctx, filepath.Join(live, name))
		if err != nil {
			return nil, err
		}
		if digest(b) != hash {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed, nil
}
func imageTagExists(ctx context.Context, d docker.Runtime, tag string) (bool, error) {
	var out strings.Builder
	if d.Runner == nil {
		return false, fmt.Errorf("Docker is unavailable")
	}
	if err := d.Runner.Run(ctx, docker.Command{Args: []string{"image", "ls", "--filter", "reference=" + tag, "--format", "{{.ID}}"}, Stdout: &out}); err != nil {
		return false, publicFailure("Cannot inspect image associations.", tag, err)
	}
	return strings.TrimSpace(out.String()) != "", nil
}

func (m Merger) Apply(ctx context.Context, p Paths, approved MergePlan) (j *Journal, err error) {
	if err = approved.Ready(); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(p.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	j, err = Load(p)
	if err != nil {
		return nil, err
	}
	if j.Merge != nil || j.Phase != "prepared" {
		return j, fmt.Errorf("merge already began; use --resume")
	}
	fresh, err := m.Plan(ctx, j, approved.Choices)
	if err != nil {
		return j, err
	}
	if fresh.Fingerprint() != approved.Fingerprint() {
		return j, fmt.Errorf("merge inputs changed; review the new plan before applying")
	}
	release, err := lockSource(ctx, &j.Inventory)
	if err != nil {
		return j, err
	}
	defer release()
	if err = checkLeases(ctx, &j.Inventory); err != nil {
		return j, err
	}
	if err = checkRuntime(ctx, DockerSource{m.Docker}, &j.Inventory, j.Excluded); err != nil {
		return j, err
	}
	if err = verifyMergeSource(ctx, j); err != nil {
		return j, err
	}
	if err = m.verifyCaptures(ctx, j, approved); err != nil {
		return j, err
	}
	j.Merge = &MergeState{Plan: approved, Approved: time.Now().UTC(), Published: map[string]bool{}, Intent: map[string]bool{}, Attempts: map[string]*Attempt{}}
	j.Phase = "merging"
	j.Failure = ""
	if err = saveJournal(j); err != nil {
		return j, err
	}
	return j, m.runMerge(ctx, j)
}
func (m Merger) Resume(ctx context.Context, p Paths) (j *Journal, err error) {
	if _, err = Load(p); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(ctx, filepath.Join(p.Work, "lock"))
	if err != nil {
		return nil, err
	}
	defer fsutil.Unlock(lock)
	j, err = Load(p)
	if err != nil {
		return nil, err
	}
	if j.Merge == nil {
		return j, fmt.Errorf("no merge was approved; --resume cannot authorize one")
	}
	if j.Phase == "completed" {
		return j, saveJournal(j)
	}
	release, err := lockSource(ctx, &j.Inventory)
	if err != nil {
		return j, err
	}
	defer release()
	if err = checkLeases(ctx, &j.Inventory); err != nil {
		return j, err
	}
	if err = checkRuntime(ctx, DockerSource{m.Docker}, &j.Inventory, j.Excluded); err != nil {
		return j, err
	}
	return j, m.runMerge(ctx, j)
}
func verifyMergeSource(ctx context.Context, j *Journal) error {
	v := j.Inventory
	v.Files = nil
	v.SourceHashes = map[string]string{}
	for path, hash := range j.Inventory.SourceHashes {
		v.SourceHashes[path] = hash
	}
	v.Directories = map[string][]string{}
	for path, names := range j.Inventory.Directories {
		v.Directories[path] = append([]string{}, names...)
	}
	if j.Merge != nil {
		for _, pub := range j.Merge.Plan.Publications {
			if original, ok := v.SourceHashes[pub.Target]; ok {
				actual, err := stateHash(ctx, pub.Target)
				if err != nil {
					return err
				}
				if actual == pub.After {
					b, err := readRegular(ctx, pub.Target)
					if err != nil {
						return err
					}
					v.SourceHashes[pub.Target] = digest(b)
					if original == "missing" {
						dir := filepath.Dir(pub.Target)
						if names, ok := v.Directories[dir]; ok {
							names = append(names, filepath.Base(pub.Target)+":"+os.FileMode(0).String())
							sort.Strings(names)
							v.Directories[dir] = names
						}
					}
				}
			}
		}
	}
	// After shared-file publication, project artifacts are live desired inputs.
	// Pending creation checks them through the normal fingerprints (and explicit
	// reapproval), while portable history and old-home data remain immutable.
	if j.Merge != nil {
		published := true
		for _, pub := range j.Merge.Plan.Publications {
			if !j.Merge.Published[pub.Key] {
				published = false
			}
		}
		if published {
			for _, item := range j.Inventory.Items {
				if item.Kind != "project" {
					continue
				}
				for path := range v.SourceHashes {
					if within(item.Path, path) {
						delete(v.SourceHashes, path)
					}
				}
				for path := range v.Directories {
					if within(item.Path, path) {
						delete(v.Directories, path)
					}
				}
			}
		}
	}
	return verifySource(ctx, &v)
}
func (m Merger) runMerge(ctx context.Context, j *Journal) (err error) {
	defer func() {
		if err != nil {
			j.Phase = "merging"
			j.Failure = err.Error()
			err = errors.Join(err, saveJournal(j))
		}
	}()
	j.Failure = ""
	if err = verifyStaging(ctx, j); err != nil {
		return err
	}
	if err = verifyMergeSource(ctx, j); err != nil {
		return err
	}
	p := j.Inventory.Paths
	plan := j.Merge.Plan
	if err = m.verifyCaptures(ctx, j, plan); err != nil {
		return err
	}
	if plan.Fresh {
		if _, e := os.Lstat(p.Destination); os.IsNotExist(e) {
			temp := j.Merge.Bootstrap
			if temp == "" {
				temp, err = os.MkdirTemp(filepath.Dir(p.Destination), ".devbox-import-home-")
				if err != nil {
					return err
				}
				j.Merge.Bootstrap = temp
				if err = saveJournal(j); err != nil {
					return err
				}
			}
			if _, err = fsutil.Dir(temp, "state", 0700); err != nil {
				return err
			}
			idPath := filepath.Join(temp, "state/installation-id")
			if b, e := readRegular(ctx, idPath); os.IsNotExist(e) {
				if err = fsutil.WriteNew(idPath, []byte(plan.Installation+"\n"), 0600); err != nil {
					return err
				}
			} else if e != nil || strings.TrimSpace(string(b)) != plan.Installation {
				return fmt.Errorf("bootstrap identity mismatch")
			}
			for _, pub := range plan.Publications {
				if pub.Item == "global:config" {
					h, e := stateHash(ctx, pub.Source)
					if e != nil || h != pub.After {
						return fmt.Errorf("bootstrap global input changed")
					}
					path := filepath.Join(temp, "config.json")
					current, e := stateHash(ctx, path)
					if e != nil {
						return e
					}
					if current == "missing" {
						b, e := readRegular(ctx, pub.Source)
						if e != nil {
							return e
						}
						if e = fsutil.WriteNew(path, b, 0600); e != nil {
							return e
						}
					} else if current != pub.After {
						return fmt.Errorf("bootstrap configuration conflict")
					}
					j.Merge.Intent[pub.Key] = true
				}
			}
			if err = saveJournal(j); err != nil {
				return err
			}
			if err = fsutil.PublishDirectory(temp, p.Destination); err != nil {
				return err
			}
			if err = m.fault("home-published"); err != nil {
				return err
			}
		} else if e != nil {
			return e
		}
	}
	b, err := readRegular(ctx, filepath.Join(p.Destination, "state/installation-id"))
	if err != nil || strings.TrimSpace(string(b)) != plan.Installation {
		return fmt.Errorf("destination installation identity changed")
	}
	// Open only after explicit approval; retain an existing identity. It seeds
	// ordinary empty directories, not migration-specific runtime records.
	st, err := store.Open(ctx, p.Destination)
	if err != nil {
		return err
	}
	release, sessionLocks, err := lockDestination(ctx, st, plan)
	if err != nil {
		return err
	}
	defer release()
	if !plan.Fresh && len(j.Merge.Intent) == 0 && len(j.Merge.Attempts) == 0 {
		_, _, snapshot, e := destinationInventory(ctx, st.Home)
		if e != nil {
			return e
		}
		if snapshot != plan.DestinationSnapshot {
			return fmt.Errorf("destination changed since approval")
		}
	}
	if err = m.destinationIdle(ctx, st, j); err != nil {
		return err
	}
	if err = m.checkAssociations(ctx, st, j, sessionLocks); err != nil {
		return err
	}
	for _, pub := range plan.Publications {
		j.Current = "publishing " + pub.Target
		j.CurrentItem = pub.Item
		if j.Merge.Published[pub.Key] {
			continue
		}
		if !j.Merge.Intent[pub.Key] {
			current, e := stateHash(ctx, pub.Target)
			if e != nil {
				return e
			}
			if current != pub.Before {
				return fmt.Errorf("destination changed before publication: %s", pub.Target)
			}
			j.Merge.Intent[pub.Key] = true
			if err = saveJournal(j); err != nil {
				return err
			}
		}
		if err = publish(ctx, j, pub); err != nil {
			return err
		}
		if err = m.fault("published:" + pub.Item); err != nil {
			return err
		}
		j.Merge.Published[pub.Key] = true
		if err = saveJournal(j); err != nil {
			return err
		}
		m.note("Published " + display(pub.Item))
	}
	e := &app.Engine{Store: st, Docker: m.Docker, UID: m.UID, GID: m.GID, Streams: docker.Streams{Out: io.Discard, Err: io.Discard}}
	for _, job := range plan.Sessions {
		if a := j.Merge.Attempts[job.Item]; a != nil && a.Phase == "done" {
			continue
		}
		j.Current = "creating " + job.Identity.Name
		j.CurrentItem = job.Item
		if err = m.importSession(ctx, j, e, sessionLocks[job.Identity.Name], job); err != nil {
			return publicFailure("Import failed for "+job.Identity.Name+": "+safeMergeError(err), job.Identity.Name, err)
		}
		m.note("Imported " + display(job.Identity.Name))
	}
	j.Phase = "completed"
	j.Failure = ""
	j.Current = "merge verified"
	j.CurrentItem = ""
	return saveJournal(j)
}
func safeMergeError(err error) string {
	var public *commanderror.Error
	if errors.As(err, &public) {
		return public.Message
	}
	return "review the source/destination state and retry; private subprocess details were withheld"
}
func lockDestination(ctx context.Context, st *store.Store, p MergePlan) (func(), map[string]*store.Locked, error) {
	files := []*os.File{}
	var locks []*store.Locked
	release := func() {
		store.CloseAll(locks)
		for n := len(files) - 1; n >= 0; n-- {
			_ = fsutil.Unlock(files[n])
		}
	}
	roots := map[string]bool{st.Home: true}
	for _, pub := range p.Publications {
		root := filepath.Dir(pub.Target)
		if pub.Directory {
			root = pub.Target
		}
		roots[root] = true
	}
	// Protect reused profile inputs as well as owners being published.
	entries, err := readEntries(filepath.Join(st.Home, "profiles"))
	if err != nil {
		return release, nil, err
	}
	for _, entry := range entries {
		roots[filepath.Join(st.Home, "profiles", entry.Name())] = true
	}
	dir, err := fsutil.Dir(st.Home, "state/locks/config", 0700)
	if err != nil {
		return release, nil, err
	}
	paths := []string{}
	for root := range roots {
		sum := sha256.Sum256([]byte(root))
		paths = append(paths, filepath.Join(dir, hex.EncodeToString(sum[:])+".lock"))
	}
	sort.Strings(paths)
	for _, path := range paths {
		f, err := fsutil.Lock(ctx, path)
		if err != nil {
			release()
			return func() {}, nil, err
		}
		files = append(files, f)
	}
	names := map[string]bool{}
	entries, err = readEntries(filepath.Join(st.Home, "sessions"))
	if err != nil {
		release()
		return func() {}, nil, err
	}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, job := range p.Sessions {
		names[job.Identity.Name] = true
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	locks, err = st.LockAll(ctx, ordered)
	if err != nil {
		release()
		return func() {}, nil, err
	}
	result := map[string]*store.Locked{}
	for _, lock := range locks {
		result[lock.Name] = lock
		if err = lock.RequireAvailable(); err == nil {
			err = lock.RequireIdle()
		}
		if err != nil {
			release()
			return func() {}, nil, err
		}
	}
	return release, result, nil
}
func (m Merger) destinationIdle(ctx context.Context, st *store.Store, j *Journal) error {
	var ids strings.Builder
	if err := m.Docker.Runner.Run(ctx, docker.Command{Args: []string{"container", "ls", "--all", "--filter", "label=" + docker.Namespace + ".installation=" + st.Installation, "--format", "{{.ID}}"}, Stdout: &ids}); err != nil {
		return publicFailure("Cannot inspect destination writers.", st.Home, err)
	}
	for _, id := range strings.Fields(ids.String()) {
		var out strings.Builder
		if err := m.Docker.Runner.Run(ctx, docker.Command{Args: []string{"container", "inspect", id}, Stdout: &out}); err != nil {
			return publicFailure("Cannot inspect destination writers.", st.Home, err)
		}
		var containers []docker.Container
		if err := json.Unmarshal([]byte(out.String()), &containers); err != nil || len(containers) != 1 {
			return fmt.Errorf("invalid destination inventory")
		}
		if containers[0].State.Running {
			allowed := false
			for _, job := range j.Merge.Plan.Sessions {
				if a := j.Merge.Attempts[job.Item]; a != nil && a.Phase != "done" && strings.TrimPrefix(containers[0].Name, "/") == job.Identity.Name {
					owner := docker.Owner{Installation: st.Installation, Session: job.ID, Workspace: job.Identity.Workspace, Slot: job.Identity.Slot}
					if err := containers[0].Verify(owner); err != nil {
						return err
					}
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("destination container is running: %s; stop it before merging shared inputs", strings.TrimPrefix(containers[0].Name, "/"))
			}
		}
	}
	return nil
}
func publish(ctx context.Context, j *Journal, p Publication) error {
	after, err := stateHash(ctx, p.Source)
	if err != nil || after != p.After {
		return fmt.Errorf("approved publication source changed: %s", p.Item)
	}
	current, err := stateHash(ctx, p.Target)
	if err != nil {
		return err
	}
	if current == p.After {
		return nil
	}
	if current != p.Before {
		return fmt.Errorf("destination changed since review: %s", p.Target)
	}
	if p.Before != "missing" {
		if p.Directory {
			return fmt.Errorf("directory replacement is not permitted")
		}
		if _, err = fsutil.Dir(filepath.Dir(p.Backup), ".", 0700); err != nil {
			return err
		}
		if existing, e := readRegular(ctx, p.Backup); e == nil {
			if digest(existing) != p.BackupHash {
				return fmt.Errorf("backup conflict")
			}
		} else if !os.IsNotExist(e) {
			return e
		} else {
			b, err := readRegular(ctx, p.Target)
			if err != nil {
				return err
			}
			if digest(b) != p.BackupHash {
				return fmt.Errorf("backup source changed")
			}
			if err = fsutil.WriteNew(p.Backup, b, 0600); err != nil {
				return err
			}
		}
	}
	if _, err = fsutil.Dir(filepath.Dir(p.Target), ".", 0700); err != nil {
		return err
	}
	if p.Directory {
		temp, err := os.MkdirTemp(filepath.Dir(p.Target), ".devbox-import-")
		if err != nil {
			return err
		}
		defer removePreview(temp)
		if err = copyTree(ctx, p.Source, temp); err != nil {
			return err
		}
		if err = syncStagedDirectories(ctx, temp); err != nil {
			return err
		}
		return fsutil.PublishDirectory(temp, p.Target)
	}
	b, err := readRegular(ctx, p.Source)
	if err != nil {
		return err
	}
	if p.Before == "missing" {
		return fsutil.WriteNew(p.Target, b, 0600)
	}
	return fsutil.Write(p.Target, b, 0600)
}

// A preserved ID may have been relocated through ordinary Neo commands after
// a crash. Never recreate its old slot or overwrite its image tag on retry.
func (m Merger) checkAssociations(ctx context.Context, st *store.Store, j *Journal, locks map[string]*store.Locked) error {
	ids := map[string]store.Record{}
	byName := map[string]store.Record{}
	for name, l := range locks {
		r, err := l.ReadRecord(ctx)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if _, exists := ids[r.ID]; exists {
			return fmt.Errorf("destination contains duplicate session IDs")
		}
		ids[r.ID] = r
		byName[name] = r
	}
	for _, job := range j.Merge.Plan.Sessions {
		a := j.Merge.Attempts[job.Item]
		if a != nil && a.Phase == "done" {
			continue
		}
		if r, exists := ids[job.ID]; exists && (r.Identity != job.Identity || a == nil) {
			return fmt.Errorf("imported session ID now belongs to another destination; refusing to recreate its old slot")
		}
		if r, exists := byName[job.Identity.Name]; exists && r.ID != job.ID {
			return fmt.Errorf("destination slot now belongs to another session")
		}
		tag := docker.Namespace + "/session:" + job.ID
		exists, err := imageTagExists(ctx, m.Docker, tag)
		if err != nil {
			return err
		}
		if exists {
			if a == nil {
				return fmt.Errorf("destination image association changed")
			}
			image, err := m.Docker.InspectImage(ctx, tag)
			if err != nil {
				return err
			}
			if err = image.Verify(st.Installation); err != nil {
				return err
			}
		}
	}
	return nil
}
func directoryIdentity(path string) (uint64, uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, 0, err
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("expected prepared directory")
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("cannot identify prepared directory")
	}
	return uint64(s.Dev), s.Ino, nil
}
func (m Merger) importSession(ctx context.Context, j *Journal, e *app.Engine, l *store.Locked, job ImportSession) error {
	var err error
	if err = l.RequireAvailable(); err != nil {
		return err
	}
	if err = l.RequireIdle(); err != nil {
		return err
	}
	a := j.Merge.Attempts[job.Item]
	r, recordErr := l.ReadRecord(ctx)
	if recordErr == nil {
		if a == nil || r.ID != job.ID || r.Identity != job.Identity {
			return fmt.Errorf("existing session is not this import")
		}
		root, pathErr := l.Path(".")
		if pathErr != nil {
			return pathErr
		}
		dev, ino, pathErr := directoryIdentity(root)
		if pathErr != nil || dev != a.Device || ino != a.Inode {
			return fmt.Errorf("committed record is outside the recorded import directory")
		}
		// A committed record is authoritative even if the following journal update
		// was interrupted. Never recopy or resynchronize it on this path.
		c, exists, err := m.Docker.Inspect(ctx, job.Identity.Name)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("committed container is missing; recover it through Neo before completing the import")
		}
		owner := docker.Owner{Installation: e.Store.Installation, Session: r.ID, Workspace: r.Identity.Workspace, Slot: r.Identity.Slot}
		if err = c.Verify(owner); err != nil {
			return err
		}
		if c.Image != r.ImageID || c.ID != r.SetupContainer {
			return fmt.Errorf("committed container identity changed")
		}
		if c.State.Running {
			if err = m.Docker.Stop(ctx, c, owner); err != nil {
				return err
			}
		}
		a.Phase = "done"
		return saveJournal(j)
	}
	if !errors.Is(recordErr, os.ErrNotExist) {
		return recordErr
	}
	root, err := l.Path(".")
	if err != nil {
		return err
	}
	if a == nil {
		if _, err = os.Lstat(root); !os.IsNotExist(err) {
			return fmt.Errorf("destination state already exists")
		}
		if _, exists, err := m.Docker.Inspect(ctx, job.Identity.Name); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("destination container already exists")
		}
		if exists, err := imageTagExists(ctx, m.Docker, docker.Namespace+"/session:"+job.ID); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("destination image association already exists")
		}
		a = &Attempt{Phase: "preparing"}
		j.Merge.Attempts[job.Item] = a
		if err = saveJournal(j); err != nil {
			return err
		}
	}
	spec, err := e.Resolve(app.Request{Workspace: job.Identity.Workspace, Profile: job.Identity.Profile, Recorded: &job.Identity, Host: m.host()})
	if err != nil {
		return publicFailure("Final configuration no longer resolves.", job.Identity.Name, err)
	}
	if spec.Fingerprints != job.Desired {
		return fmt.Errorf("final inputs changed since approval")
	}
	if _, err = os.Lstat(root); err == nil {
		dev, ino, err := directoryIdentity(root)
		if err != nil || dev != a.Device || ino != a.Inode {
			return fmt.Errorf("uncommitted destination directory is not the recorded import attempt")
		}
		c, exists, err := m.Docker.Inspect(ctx, job.Identity.Name)
		if err != nil {
			return err
		}
		if exists {
			owner := docker.Owner{Installation: e.Store.Installation, Session: job.ID, Workspace: job.Identity.Workspace, Slot: job.Identity.Slot}
			if err = c.Verify(owner); err != nil {
				return err
			}
			if err = m.Docker.Stop(ctx, c, owner); err != nil {
				return err
			}
			if err = m.Docker.Remove(ctx, c, owner); err != nil {
				return err
			}
		}
		if err = l.Delete(); err != nil {
			return err
		}
		a.Temporary = ""
		a.Device = 0
		a.Inode = 0
		a.Phase = "preparing"
		if err = saveJournal(j); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, e := os.Lstat(a.Temporary); a.Temporary != "" && os.IsNotExist(e) {
		a.Temporary = ""
		a.Device = 0
		a.Inode = 0
		a.Phase = "preparing"
	}
	if a.Temporary == "" {
		temp, err := os.MkdirTemp(e.Store.Home, ".import-")
		if err != nil {
			return err
		}
		a.Temporary = temp
		a.Device, a.Inode, err = directoryIdentity(temp)
		if err != nil {
			return err
		}
		a.Phase = "preparing"
		if err = saveJournal(j); err != nil {
			return err
		}
	}
	dev, ino, err := directoryIdentity(a.Temporary)
	if err != nil || dev != a.Device || ino != a.Inode {
		return fmt.Errorf("prepared directory identity changed")
	}
	if err = copySession(ctx, j, job, a.Temporary); err != nil {
		return err
	}
	a.Device, a.Inode, err = directoryIdentity(a.Temporary)
	if err != nil {
		return err
	}
	a.Phase = "creating"
	if err = saveJournal(j); err != nil {
		return err
	}
	if err = fsutil.PublishDirectory(a.Temporary, root); err != nil {
		return err
	}
	if err = m.fault("session-published:" + job.Item); err != nil {
		return err
	}
	activity := job.Activity
	action := job.Action
	if activity.IsZero() {
		activity = j.Merge.Approved
		action = "create"
	}
	r, c, err := e.CreatePrepared(ctx, l, spec, app.CreationIdentity{ID: job.ID, Created: job.Created, Activity: activity, Action: action})
	if err != nil {
		return err
	}
	if err = m.fault("record-committed:" + job.Item); err != nil {
		return err
	}
	owner := docker.Owner{Installation: e.Store.Installation, Session: r.ID, Workspace: r.Identity.Workspace, Slot: r.Identity.Slot}
	if err = m.Docker.Stop(ctx, c, owner); err != nil {
		return err
	}
	a.Phase = "done"
	return saveJournal(j)
}
func copySession(ctx context.Context, j *Journal, job ImportSession, target string) error {
	var err error
	item := j.Inventory.item(job.Item)
	if item == nil {
		return fmt.Errorf("unknown session import")
	}
	prefix := filepath.Join("sessions", item.Target) + string(filepath.Separator)
	for _, file := range j.Inventory.Files {
		if file.Item != job.Item || !strings.HasPrefix(file.Relative, prefix) {
			continue
		}
		rel := strings.TrimPrefix(file.Relative, prefix)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("unsafe prepared path")
		}
		to := filepath.Join(target, rel)
		if _, err := fsutil.Path(filepath.Dir(to), "."); err != nil {
			return err
		}
		if file.Mode.IsDir() {
			if _, err = fsutil.Dir(target, rel, 0700); err != nil {
				return err
			}
			continue
		}
		if _, err = fsutil.Dir(filepath.Dir(to), ".", 0700); err != nil {
			return err
		}
		if _, e := os.Lstat(to); e == nil {
			ok, e := matches(ctx, to, file)
			if e != nil || !ok {
				return fmt.Errorf("prepared file changed: %s", to)
			}
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		file.Source = filepath.Join(j.Inventory.Paths.Work, "staged-home", file.Relative)
		file.Data = nil
		if err = copyFile(ctx, to, file, nil); err != nil {
			return err
		}
	}
	if capture, ok := j.Merge.Plan.Captures[job.Item]; ok && !contains(j.Merge.Plan.Choices.OmitConfig, job.Item) {
		current, err := stateHash(ctx, capture.Root)
		if err != nil || current != capture.Hash {
			return fmt.Errorf("captured configuration changed")
		}
		destination := filepath.Join(target, "harnesses/opencode/stores/config")
		if _, err = os.Lstat(destination); os.IsNotExist(err) {
			if err = copyTree(ctx, capture.Root, destination); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			a, e := stateHash(ctx, destination)
			if e != nil || a != capture.Hash {
				return fmt.Errorf("prepared OpenCode config changed")
			}
		}
	}
	known := map[string]bool{".": true}
	add := func(rel string) {
		for p := rel; p != "."; p = filepath.Dir(p) {
			known[p] = true
		}
	}
	for _, file := range j.Inventory.Files {
		if file.Item == job.Item && strings.HasPrefix(file.Relative, prefix) {
			add(strings.TrimPrefix(file.Relative, prefix))
		}
	}
	if c, ok := j.Merge.Plan.Captures[job.Item]; ok && !contains(j.Merge.Plan.Choices.OmitConfig, job.Item) {
		if err = filepath.WalkDir(c.Root, func(path string, e os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, e2 := filepath.Rel(c.Root, path)
			if e2 != nil {
				return e2
			}
			add(filepath.Join("harnesses/opencode/stores/config", rel))
			return nil
		}); err != nil {
			return err
		}
	}
	if err = filepath.WalkDir(target, func(path string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e2 := filepath.Rel(target, path)
		if e2 != nil {
			return e2
		}
		if !known[rel] {
			return fmt.Errorf("unreviewed prepared entry: %s", path)
		}
		return ctx.Err()
	}); err != nil {
		return err
	}
	return syncStagedDirectories(ctx, target)
}
