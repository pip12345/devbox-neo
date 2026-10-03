// Package migration detects required updates and executes approved conversions.
// Ordinary session readers never recognize the preceding format or namespace.
package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

const oldNamespace = "devbox-rewrite"

type Session struct {
	ID            string `json:"session_id"`
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
	oldTag    string
	container docker.Container
	tagged    bool
}

// Run holds the complete lock set through preflight and conversion. Each record
// is published only after its linked old resources are absent, so a partial run
// can retry without a second journal or recopying any harness state.
func Run(ctx context.Context, home string, runtime docker.Runtime, apply bool) (Result, error) {
	result := Result{Sessions: []Session{}, Applied: apply}
	installationPath, err := fsutil.Path(home, "state/installation-id")
	if err != nil {
		return result, err
	}
	data, err := os.ReadFile(installationPath)
	if err != nil {
		return result, fmt.Errorf("cutover requires an existing Devbox installation: %w", err)
	}
	installation := strings.TrimSpace(string(data))
	if !environment.IsSessionTarget(installation) {
		return result, fmt.Errorf("invalid installation identity")
	}
	s := &store.Store{Home: home, Installation: installation}
	names, err := s.LockNames(ctx)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(names)
	transfers, err := s.Transfers()
	if err != nil {
		return result, err
	}
	if len(transfers) != 0 {
		return result, fmt.Errorf("finish pending transfers before runtime cutover")
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		return result, err
	}
	var directories []string
	ids := map[string]string{}
	seenIDs := map[string]bool{}
	seenBindings := map[environment.Binding]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return result, fmt.Errorf("unexpected session entry %s", entry.Name())
		}
		r, _, old, err := readCandidate(home, entry.Name())
		// An interrupted allocation has no saved contract to convert. Leave
		// its files for explicit cleanup, under the namespace lock.
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return result, err
		}
		if seenIDs[r.ID] || seenBindings[r.Settings.Binding] {
			return result, fmt.Errorf("duplicate session identity or workspace/name")
		}
		seenIDs[r.ID], seenBindings[r.Settings.Binding] = true, true
		if !old {
			continue
		}
		directories = append(directories, entry.Name())
		ids[entry.Name()] = r.ID
	}
	locks, err := s.LockAll(ctx, directories, ids)
	if err != nil {
		return result, err
	}
	defer store.CloseAll(locks)
	var work []candidate
	for _, lock := range locks {
		if err := lock.RequireIdle(); err != nil {
			return result, err
		}
		r, tag, old, err := readCandidate(home, lock.Name)
		if err != nil {
			return result, err
		}
		if !old || r.ID != lock.ID {
			return result, fmt.Errorf("session changed during cutover; retry preview")
		}
		c, exists, err := runtime.InspectID(ctx, r.Applied.SetupContainer)
		if err != nil {
			return result, err
		}
		if exists {
			if err = verifyContainer(c, installation, r); err != nil {
				return result, err
			}
		} else {
			// A reused readable name is not an old instance we may remove.
			_, occupied, err := runtime.Inspect(ctx, r.Applied.Creation.Name)
			if err != nil {
				return result, err
			}
			if occupied {
				return result, fmt.Errorf("recorded container name is occupied by another instance")
			}
		}
		image, tagged, err := runtime.TaggedImage(ctx, tag)
		if err != nil {
			return result, err
		}
		if tagged {
			if image.ID != r.Applied.ImageID || !oldImageOwned(image, installation) {
				return result, fmt.Errorf("old image tag has a different owner or image: %s", tag)
			}
		}
		item := Session{ID: r.ID, Workspace: r.Settings.Workspace, LocalName: r.Settings.LocalName}
		if exists {
			item.Container = c.ID
			item.ContainerName = strings.TrimPrefix(c.Name, "/")
		}
		if tagged {
			item.ImageTag = tag
		}
		result.Sessions = append(result.Sessions, item)
		work = append(work, candidate{r, tag, c, tagged})
	}
	if !apply {
		return result, nil
	}
	for i, item := range work {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if item.container.ID != "" {
			c, exists, err := runtime.InspectID(ctx, item.container.ID)
			if err != nil {
				return result, err
			}
			if exists {
				if err = verifyContainer(c, installation, item.record); err != nil {
					return result, err
				}
				if c.State.Running {
					if err = runtime.Runner.Run(ctx, docker.Command{Args: []string{"stop", "--time", "10", c.ID}}); err != nil {
						return result, err
					}
				}
				if err = runtime.Runner.Run(ctx, docker.Command{Args: []string{"rm", c.ID}}); err != nil {
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
				if image.ID != item.record.Applied.ImageID || !oldImageOwned(image, installation) {
					return result, fmt.Errorf("old image tag changed during cutover")
				}
				if err = runtime.Runner.Run(ctx, docker.Command{Args: []string{"image", "rm", item.oldTag}}); err != nil {
					return result, err
				}
			}
		}
		if err := locks[i].Save(item.record); err != nil {
			return result, err
		}
	}
	return result, nil
}

func readCandidate(home, directory string) (store.Record, string, bool, error) {
	var r store.Record
	path, err := fsutil.Path(home, filepath.Join("sessions", directory, "session.json"))
	if err != nil {
		return r, "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, "", false, err
	}
	// Decode checks duplicate keys; the subsequent typed reader is deliberately
	// scoped to this conversion service, not a permissive ordinary store reader.
	var fields map[string]json.RawMessage
	if err = config.Decode(data, &fields); err != nil {
		return r, "", false, err
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return r, "", false, err
	}
	r.Directory = directory
	if r.Version == store.RecordVersion {
		if err := config.Decode(data, &r); err != nil {
			return r, "", false, err
		}
		return r, "", false, r.Validate(directory)
	}
	if r.Version != 6 || r.Applied.ImageTag != oldNamespace+"/session:"+r.ID {
		return r, "", false, fmt.Errorf("cutover accepts only schema 6 with the devbox-rewrite namespace: %s", directory)
	}
	var legacy struct {
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
	if err = json.Unmarshal(data, &legacy); err != nil {
		return r, "", false, err
	}
	if len(legacy.Applied.Inputs.Image.Stages) != len(r.Applied.Inputs.Image.Stages) || legacy.Applied.Inputs.Runtime.Files == nil {
		return r, "", false, fmt.Errorf("incomplete preceding input inventory")
	}
	contexts := make([]map[string]environment.FileInput, len(legacy.Applied.Inputs.Image.Stages))
	for i, stage := range legacy.Applied.Inputs.Image.Stages {
		contexts[i] = stage.Context
		if stage.Context == nil {
			return r, "", false, fmt.Errorf("missing preceding build context")
		}
		r.Applied.Inputs.Image.Stages[i].Context = environment.FilesDigest(stage.Context)
	}
	if legacyFingerprints(r, contexts, legacy.Applied.Inputs.Runtime.Files) != r.Applied.Fingerprints {
		return r, "", false, fmt.Errorf("preceding record inputs do not match committed fingerprints: %s", directory)
	}
	r.Applied.Inputs.Runtime.Files = environment.FilesDigest(legacy.Applied.Inputs.Runtime.Files)
	oldTag := r.Applied.ImageTag
	r.Version = store.RecordVersion
	r.Applied.ImageTag = environment.ImageTag(r.Settings.Workspace, r.Settings.LocalName, r.ID)
	r.Applied.Fingerprints = r.Applied.Inputs.FingerprintsFor(r.Applied.ImageID)
	r.Applied.Creation.RawArgs = r.Applied.Inputs.Container.RawArgs
	return r, oldTag, true, r.Validate(directory)
}

func oldImageOwned(image docker.Image, installation string) bool {
	labels := image.Config.Labels
	return labels[oldNamespace+".managed"] == "true" && labels[oldNamespace+".ownership"] == "1" && labels[oldNamespace+".installation"] == installation
}

func verifyContainer(c docker.Container, installation string, r store.Record) error {
	labels := c.Config.Labels
	if c.ID != r.Applied.SetupContainer || c.Image != r.Applied.ImageID || labels[oldNamespace+".managed"] != "true" || labels[oldNamespace+".ownership"] != "1" || labels[oldNamespace+".installation"] != installation || labels[oldNamespace+".session"] != r.ID {
		return fmt.Errorf("old container differs from the recorded identity/owner: %s", c.ID)
	}
	return nil
}
