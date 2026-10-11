// Package migration executes consented format changes. Preceding-format
// readers exist only here; ordinary services accept only the current model.
package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

const oldNamespace = "devbox-rewrite"

func legacyID(id string) bool { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }

type Session struct {
	ID            string `json:"session_id"`
	PreviousID    string `json:"previous_id,omitempty"`
	Workspace     string `json:"workspace"`
	LocalName     string `json:"local_name"`
	Container     string `json:"container,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ImageTag      string `json:"image_tag,omitempty"`
}
type Result struct {
	Sessions []Session `json:"sessions"`
	Applied  bool      `json:"applied"`
}

type candidate struct {
	record    store.Record
	directory string
	oldID     string
	oldTag    string
	namespace string
	container docker.Container
	tagged    bool
}

// Defaults publish before record/identity changes. Thus an interrupted run
// never loses the old-to-new default association: a retry either reads original
// full IDs or a complete checked short-ID mapping. Record moves are themselves
// recoverable from their preceding header until the final atomic publication.
func Run(ctx context.Context, home string, runtime docker.Runtime, apply bool) (Result, error) {
	result := Result{Sessions: []Session{}, Applied: apply}
	installationPath, err := fsutil.Path(home, "state/installation-id")
	if err != nil {
		return result, err
	}
	data, err := os.ReadFile(installationPath)
	if err != nil {
		return result, fmt.Errorf("conversion requires an existing installation: %w", err)
	}
	installation := strings.TrimSpace(string(data))
	if !legacyID(installation) {
		return result, fmt.Errorf("invalid installation identity")
	}
	s := &store.Store{Home: home, Installation: installation}
	names, err := s.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(names)
	if err := requireResolvedTransfers(home); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		return result, err
	}
	var work []candidate
	lockIDs := map[string]bool{}
	origins := map[string]string{}
	bindings := map[environment.Binding]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return result, fmt.Errorf("unexpected session entry %s", entry.Name())
		}
		item, old, err := readConversionCandidate(home, entry.Name())
		if os.IsNotExist(err) {
			continue // incomplete allocations retain their files for explicit cleanup
		}
		if err != nil {
			return result, err
		}
		if _, found := origins[item.record.ID]; found || bindings[item.record.Settings.Binding] {
			return result, fmt.Errorf("conversion destinations or workspace/name bindings overlap")
		}
		origin := item.record.ID
		if old {
			origin = item.oldID
		}
		origins[item.record.ID] = origin
		bindings[item.record.Settings.Binding] = true
		if !old {
			continue
		}
		if item.directory != item.record.ID {
			p, err := fsutil.Path(home, filepath.Join("sessions", item.record.ID))
			if err != nil {
				return result, err
			}
			if _, err := os.Lstat(p); err == nil || !os.IsNotExist(err) {
				return result, fmt.Errorf("conversion destination already exists: %s", p)
			}
		}
		lockIDs[item.oldID], lockIDs[item.record.ID] = true, true
		work = append(work, item)
	}
	var ids []string
	for id := range lockIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var locks []*os.File
	defer func() {
		for i := len(locks) - 1; i >= 0; i-- {
			fsutil.Unlock(locks[i])
		}
	}()
	for _, id := range ids {
		dir, err := fsutil.Dir(home, "state/locks/sessions", 0700)
		if err != nil {
			return result, err
		}
		lock, err := fsutil.Lock(ctx, filepath.Join(dir, id+".lock"))
		if err != nil {
			return result, err
		}
		locks = append(locks, lock)
		path, err := fsutil.Path(home, filepath.Join("state/leases", id))
		if err != nil {
			return result, err
		}
		active, err := store.InspectLeases(path)
		if err != nil {
			return result, err
		}
		if len(active) != 0 {
			return result, fmt.Errorf("finish active commands before converting session %s", id)
		}
	}
	for i := range work {
		item := &work[i]
		fresh, old, err := readConversionCandidate(home, item.directory)
		if err != nil || !old || fresh.oldID != item.oldID || fresh.record.ID != item.record.ID {
			return result, fmt.Errorf("session changed during conversion preflight: %s", item.directory)
		}
		c, exists, err := runtime.InspectID(ctx, item.record.Applied.ContainerID)
		if err != nil {
			return result, err
		}
		if exists {
			if err := verifyContainer(c, installation, *item); err != nil {
				return result, err
			}
			item.container = c
		}
		image, tagged, err := runtime.TaggedImage(ctx, item.oldTag)
		if err != nil {
			return result, err
		}
		if tagged && (image.ID != item.record.Applied.ImageID || !imageOwned(image, installation, item.namespace)) {
			return result, fmt.Errorf("recorded image alias has another owner or image: %s", item.oldTag)
		}
		item.tagged = tagged
		if item.namespace == docker.Namespace {
			alias, exists, err := runtime.TaggedImage(ctx, item.record.Applied.ImageTag)
			if err != nil {
				return result, err
			}
			if exists && (alias.ID != item.record.Applied.ImageID || !imageOwned(alias, installation, docker.Namespace)) {
				return result, fmt.Errorf("new image alias is occupied")
			}
		}
		result.Sessions = append(result.Sessions, Session{ID: item.record.ID, PreviousID: item.oldID, Workspace: item.record.Settings.Workspace,
			LocalName: item.record.Settings.LocalName, Container: item.container.ID, ContainerName: strings.TrimPrefix(item.container.Name, "/"), ImageTag: item.oldTag})
	}
	// External bind users cannot follow a directory move. Discovery blocks the
	// conversion rather than authorizing removal of unrelated containers.
	if len(work) > 0 {
		containers, err := runtime.AllContainers(ctx)
		if err != nil {
			return result, err
		}
		linked := map[string]bool{}
		for _, item := range work {
			linked[item.container.ID] = true
		}
		for _, c := range containers {
			if linked[c.ID] {
				continue
			}
			for _, mount := range c.Mounts {
				if mount.Type != "bind" {
					continue
				}
				for _, item := range work {
					root := filepath.Join(home, "sessions", item.directory)
					if pathContains(root, mount.Source) || pathContains(mount.Source, root) {
						return result, fmt.Errorf("container %s still mounts saved session files; remove that bind before converting", c.Name)
					}
				}
			}
		}
	}
	defaults, err := prepareDefaults(ctx, home, origins)
	if err != nil {
		return result, err
	}
	defer defaults.close()
	if !apply {
		return result, nil
	}
	if err := defaults.publish(ctx); err != nil {
		return result, err
	}
	for _, item := range work {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if item.container.ID != "" {
			c, exists, err := runtime.InspectID(ctx, item.container.ID)
			if err != nil {
				return result, err
			}
			if exists {
				if err := verifyContainer(c, installation, item); err != nil {
					return result, err
				}
				if c.State.Running {
					if err := runtime.Runner.Run(ctx, docker.Command{Args: []string{"stop", "--time", "10", c.ID}}); err != nil {
						return result, err
					}
				}
				if err := runtime.Runner.Run(ctx, docker.Command{Args: []string{"rm", c.ID}}); err != nil {
					return result, err
				}
			}
		}
		if item.tagged {
			image, exists, err := runtime.TaggedImage(ctx, item.oldTag)
			if err != nil {
				return result, err
			}
			if exists {
				if image.ID != item.record.Applied.ImageID || !imageOwned(image, installation, item.namespace) {
					return result, fmt.Errorf("image alias changed during conversion")
				}
				if item.namespace == docker.Namespace {
					if err := runtime.Tag(ctx, image.ID, item.record.Applied.ImageTag, installation); err != nil {
						return result, err
					}
				}
				if err := runtime.Runner.Run(ctx, docker.Command{Args: []string{"image", "rm", item.oldTag}}); err != nil {
					return result, err
				}
			}
		}
		root, err := fsutil.Path(home, filepath.Join("sessions", item.directory))
		if err != nil {
			return result, err
		}
		if _, err := fsutil.Dir(root, store.InitialStateTree, 0700); err != nil {
			return result, err
		}
		if err := moveIfPresent(root, "harnesses", filepath.Join(store.InitialStateTree, "harnesses")); err != nil {
			return result, err
		}
		if err := moveIfPresent(root, "runtime/ssh", sshshare.RelativeRoot); err != nil {
			return result, err
		}
		target, err := fsutil.Path(home, filepath.Join("sessions", item.record.ID))
		if err != nil {
			return result, err
		}
		if root != target {
			if err := os.Rename(root, target); err != nil {
				return result, err
			}
			if err := syncDirectory(filepath.Dir(target)); err != nil {
				return result, err
			}
		}
		leaseRoot, err := fsutil.Path(home, "state/leases")
		if err != nil {
			return result, err
		}
		if err := moveIfPresent(leaseRoot, item.oldID, item.record.ID); err != nil {
			return result, err
		}
		if err := fsutil.JSON(filepath.Join(target, "session.json"), item.record); err != nil {
			return result, err
		}
	}
	return result, nil
}

func readConversionCandidate(home, directory string) (candidate, bool, error) {
	item := candidate{directory: directory}
	path, err := fsutil.Path(home, filepath.Join("sessions", directory, "session.json"))
	if err != nil {
		return item, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return item, false, err
	}
	var fields map[string]json.RawMessage
	if err := config.Decode(data, &fields); err != nil {
		return item, false, err
	}
	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil {
		return item, false, err
	}
	if version == store.RecordVersion {
		if err := config.Decode(data, &item.record); err != nil {
			return item, false, err
		}
		item.record.Directory = directory
		return item, false, item.record.Validate(directory)
	}
	if version != 6 && version != 7 {
		return item, false, fmt.Errorf("unsupported preceding session schema %d", version)
	}
	var applied map[string]json.RawMessage
	if err := json.Unmarshal(fields["applied"], &applied); err != nil {
		return item, false, err
	}
	applied["container_id"] = applied["setup_container"]
	delete(applied, "setup_container")
	fields["applied"], err = json.Marshal(applied)
	if err != nil {
		return item, false, err
	}
	converted, err := json.Marshal(fields)
	if err != nil {
		return item, false, err
	}
	// Unknown preceding recovery fields are discarded by this version-specific
	// reader, never accepted by the ordinary current-format decoder.
	if err := json.Unmarshal(converted, &item.record); err != nil {
		return item, false, err
	}
	r := &item.record
	item.oldID, item.oldTag = r.ID, r.Applied.ImageTag
	if !legacyID(item.oldID) {
		return item, false, fmt.Errorf("invalid preceding session identity")
	}
	item.namespace = docker.Namespace
	if version == 6 {
		item.namespace = oldNamespace
		if item.oldTag != oldNamespace+"/session:"+item.oldID {
			return item, false, fmt.Errorf("invalid preceding image namespace")
		}
		var preceding struct {
			Applied struct {
				Inputs struct {
					Image struct {
						Stages []struct {
							Context map[string]environment.FileInput `json:"context"`
						} `json:"stages"`
					} `json:"image"`
					Runtime struct {
						Files map[string]environment.FileInput `json:"files"`
					} `json:"runtime"`
				} `json:"inputs"`
			} `json:"applied"`
		}
		if err := json.Unmarshal(data, &preceding); err != nil {
			return item, false, err
		}
		if len(preceding.Applied.Inputs.Image.Stages) != len(r.Applied.Inputs.Image.Stages) || preceding.Applied.Inputs.Runtime.Files == nil {
			return item, false, fmt.Errorf("incomplete preceding input inventory")
		}
		contexts := make([]map[string]environment.FileInput, len(preceding.Applied.Inputs.Image.Stages))
		for i, stage := range preceding.Applied.Inputs.Image.Stages {
			if stage.Context == nil {
				return item, false, fmt.Errorf("missing preceding build context")
			}
			contexts[i] = stage.Context
			r.Applied.Inputs.Image.Stages[i].Context = environment.FilesDigest(stage.Context)
		}
		if legacyFingerprints(*r, contexts, preceding.Applied.Inputs.Runtime.Files) != r.Applied.Fingerprints {
			return item, false, fmt.Errorf("preceding inputs differ from committed fingerprints")
		}
		r.Applied.Inputs.Runtime.Files = environment.FilesDigest(preceding.Applied.Inputs.Runtime.Files)
		r.Applied.Fingerprints = r.Applied.Inputs.FingerprintsFor(r.Applied.ImageID)
	} else if !strings.HasPrefix(item.oldTag, docker.Namespace+"/session:") || !strings.HasSuffix(item.oldTag, "-"+item.oldID) {
		return item, false, fmt.Errorf("invalid preceding image namespace")
	}
	r.ID, r.Directory, r.StateTree, r.Version = item.oldID[:12], item.oldID[:12], store.InitialStateTree, store.RecordVersion
	r.Applied.Creation.Name = environment.ResourceName(r.ID, "converted")
	r.Applied.ImageTag = environment.ImageTag(r.Applied.Creation.Name)
	r.Applied.Creation.RawArgs = r.Applied.Inputs.Container.RawArgs
	root := filepath.Join(home, "sessions", r.ID)
	for i := range r.Applied.Creation.Mounts {
		m := &r.Applied.Creation.Mounts[i]
		if m.Target == sshshare.Mount {
			m.Source = filepath.Join(root, sshshare.RelativeRoot)
		}
		for _, st := range r.Applied.Stores {
			if st.Scope == "environment" && m.Target == st.Target {
				m.Source = filepath.Join(root, r.StateTree, "harnesses", r.Applied.Definition.Name, "stores", st.Name)
			}
		}
	}
	return item, true, r.Validate(r.ID)
}

func imageOwned(image docker.Image, installation, namespace string) bool {
	labels := image.Config.Labels
	return labels[namespace+".managed"] == "true" && labels[namespace+".ownership"] == "1" && labels[namespace+".installation"] == installation
}
func verifyContainer(c docker.Container, installation string, item candidate) error {
	labels := c.Config.Labels
	if c.ID != item.record.Applied.ContainerID || c.Image != item.record.Applied.ImageID ||
		labels[item.namespace+".managed"] != "true" || labels[item.namespace+".ownership"] != "1" ||
		labels[item.namespace+".installation"] != installation || labels[item.namespace+".session"] != item.oldID {
		return fmt.Errorf("container differs from recorded instance/owner: %s", c.ID)
	}
	return nil
}

func moveIfPresent(root, from, to string) error {
	source, err := fsutil.Path(root, from)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	target, err := fsutil.Path(root, to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("conversion destination already exists: %s", target)
	}
	if _, err := fsutil.Dir(root, filepath.Dir(to), 0700); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	if filepath.Dir(source) != filepath.Dir(target) {
		return syncDirectory(filepath.Dir(source))
	}
	return nil
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func pathContains(root, path string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
