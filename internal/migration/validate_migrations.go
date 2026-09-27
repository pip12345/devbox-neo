package migration

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"devbox/internal/config"
	"devbox/internal/environment"
)

func hashString(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}
func validateChoices(j *Journal, c MergeChoices) error {
	for kind, keys := range map[string][]string{"profile": c.Reuse, "project": c.Projects, "auth": c.ReplaceAuth, "session": c.OmitConfig} {
		for _, key := range keys {
			item := j.Inventory.item(key)
			if item == nil || item.Kind != kind {
				return fmt.Errorf("unknown %s decision: %s", kind, display(key))
			}
		}
	}
	for old, name := range c.Rename {
		if j.Inventory.item("profile:"+old) == nil || !config.Name.MatchString(name) {
			return fmt.Errorf("invalid profile rename decision")
		}
	}
	return nil
}

// Resume validates the recorded effects against source owners, not just JSON
// shape. A damaged target path must never become permission to write elsewhere.
func validateMerge(j *Journal) error {
	s := j.Merge
	if s == nil {
		return nil
	}
	p := s.Plan
	home := j.Inventory.Paths.Destination
	work := j.Inventory.Paths.Work
	if !idPattern.MatchString(p.Installation) || s.Approved.IsZero() || s.Published == nil || s.Intent == nil || s.Attempts == nil {
		return fmt.Errorf("incomplete merge state")
	}
	if err := validateChoices(j, p.Choices); err != nil {
		return err
	}
	if err := p.Ready(); err != nil {
		return fmt.Errorf("journal contains an unapproved merge plan")
	}
	pubs := map[string]bool{}
	for _, pub := range p.Publications {
		item := j.Inventory.item(pub.Item)
		if item == nil || p.Excluded[pub.Item] != "" {
			return fmt.Errorf("invalid publication owner")
		}
		target := ""
		directory := false
		switch item.Kind {
		case "global":
			target = importedGlobalPath(home)
		case "profile":
			name := item.Name
			if p.Choices.Rename[name] != "" {
				name = p.Choices.Rename[name]
			}
			if !config.Name.MatchString(name) {
				return fmt.Errorf("invalid profile target")
			}
			target = filepath.Join(home, "configs", name)
			directory = true
		case "project":
			if !contains(p.Choices.Projects, item.Key) {
				return fmt.Errorf("unapproved project edit")
			}
			target = filepath.Join(item.Path, "config.json")
		case "auth":
			target = filepath.Join(home, "auth", item.Harness, "auth.json")
			if pub.Before != "missing" && !contains(p.Choices.ReplaceAuth, item.Key) {
				return fmt.Errorf("unapproved auth replacement")
			}
		case "cache":
			target = filepath.Join(home, "cache/harnesses", item.Name)
			directory = true
		default:
			return fmt.Errorf("invalid publication kind")
		}
		if target != pub.Target || pub.Directory != directory || pub.Key != digest([]byte(target)) || pubs[pub.Key] || !hashString(pub.After) || (pub.Before != "missing" && !hashString(pub.Before)) {
			return fmt.Errorf("invalid publication identity")
		}
		pubs[pub.Key] = true
		source := stagedItemRoot(j, *item)
		if item.Kind == "project" {
			source = filepath.Join(source, "config.json")
		}
		if item.Kind == "global" {
			if filepath.Dir(pub.Source) != filepath.Join(work, "merge-inputs") || !strings.HasSuffix(pub.Source, ".json") || !hashString(strings.TrimSuffix(filepath.Base(pub.Source), ".json")) {
				return fmt.Errorf("invalid global merge input")
			}
		} else if pub.Source != source {
			return fmt.Errorf("publication source escaped staging")
		}
		if directory && pub.Before != "missing" {
			return fmt.Errorf("directory replacement is forbidden")
		}
		if pub.Before != "missing" {
			parent := "destination-backups"
			if item.Kind == "project" {
				parent = "project-backups"
			}
			if pub.Backup != filepath.Join(work, parent, pub.Key) || !hashString(pub.BackupHash) {
				return fmt.Errorf("invalid replacement backup")
			}
		} else if pub.Backup != "" {
			return fmt.Errorf("unexpected replacement backup")
		}
	}
	for key := range s.Intent {
		if !pubs[key] {
			return fmt.Errorf("unknown publication intent")
		}
	}
	for key := range s.Published {
		if !pubs[key] || !s.Intent[key] {
			return fmt.Errorf("publication lacks recorded intent")
		}
	}
	jobs := map[string]bool{}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, job := range p.Sessions {
		item := j.Inventory.item(job.Item)
		if item == nil || item.Kind != "session" || p.Excluded[job.Item] != "" || jobs[job.Item] {
			return fmt.Errorf("invalid imported session owner")
		}
		profile := item.Profile
		if profile == "" {
			profile = item.SourceProfile
		}
		if p.Choices.Rename[profile] != "" {
			profile = p.Choices.Rename[profile]
		}
		if profile != "" && !config.Name.MatchString(profile) {
			return fmt.Errorf("invalid imported profile")
		}
		if err := job.Identity.Validate(); err != nil {
			return err
		}
		localName := importLocalName(*item, profile)
		expected := environment.Identity{Name: environment.ResourceName(item.Workspace, localName, item.SessionID), Binding: environment.Binding{Workspace: item.Workspace, LocalName: localName}}
		if !reflect.DeepEqual(job.Sources, importReferences(home, *item, profile)) {
			return fmt.Errorf("imported session source chain differs from its approved selection")
		}
		if job.Identity != expected || job.ID != item.SessionID || !idPattern.MatchString(job.ID) || job.Created.IsZero() || !hashString(job.Desired.Image) || !hashString(job.Desired.Container) || !hashString(job.Desired.Runtime) || names[expected.Name] || ids[job.ID] {
			return fmt.Errorf("invalid imported session identity")
		}
		jobs[job.Item] = true
		names[expected.Name] = true
		ids[job.ID] = true
	}
	for item, c := range p.Captures {
		if !jobs[item] || filepath.Dir(c.Root) != work || !strings.HasPrefix(filepath.Base(c.Root), "capture-") || !hashString(c.Hash) || c.Container == "" {
			return fmt.Errorf("invalid captured config binding")
		}
	}
	for item, a := range s.Attempts {
		if !jobs[item] || a == nil || (a.Phase != "preparing" && a.Phase != "creating" && a.Phase != "done") {
			return fmt.Errorf("invalid import attempt")
		}
		if a.Temporary != "" && (filepath.Dir(a.Temporary) != home || !strings.HasPrefix(filepath.Base(a.Temporary), ".import-")) {
			return fmt.Errorf("prepared directory escaped destination owner")
		}
		if a.Phase != "preparing" && (a.Inode == 0 || a.Device == 0) {
			return fmt.Errorf("import attempt lacks directory identity")
		}
	}
	if s.Bootstrap != "" && (filepath.Dir(s.Bootstrap) != filepath.Dir(home) || !strings.HasPrefix(filepath.Base(s.Bootstrap), ".devbox-import-home-") || within(j.Inventory.Paths.Source, s.Bootstrap) || within(s.Bootstrap, j.Inventory.Paths.Source)) {
		return fmt.Errorf("invalid bootstrap directory")
	}
	return nil
}
