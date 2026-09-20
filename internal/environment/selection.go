package environment

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
)

func Slot(profile string, project bool) string {
	if profile == "" {
		if project {
			return "project"
		}
		return ""
	}
	slot := "profile-" + profile
	if project {
		slot += ".project"
	}
	return slot
}

func ParseSlot(slot string) (profile string, project bool, err error) {
	if slot == "project" {
		return "", true, nil
	}
	profile, ok := strings.CutPrefix(slot, "profile-")
	if !ok {
		return "", false, fmt.Errorf("invalid session slot")
	}
	profile, project = strings.CutSuffix(profile, ".project")
	if !config.Name.MatchString(profile) {
		return "", false, fmt.Errorf("invalid profile in session slot")
	}
	return profile, project, nil
}

// SelectionSources expands frontend selection into generic directory inputs.
// Saved environments use their recorded Sources instead of rediscovering these.
func SelectionSources(home string, id Identity) []config.Source {
	var sources []config.Source
	if id.Profile != "" {
		sources = append(sources, config.Source{Label: "profile", Path: filepath.Join(home, "profiles", id.Profile)})
	}
	if id.Project {
		root := id.ProjectDir
		if root == "" {
			root = filepath.Join(id.Workspace, ".devbox")
		}
		sources = append(sources, config.Source{Label: "project", Path: root})
	}
	return sources
}

func (id Identity) ValidateSlot() error {
	if id.Slot == "" || id.Slot != Slot(id.Profile, id.Project) {
		return fmt.Errorf("session slot does not match its profile/project selection")
	}
	if id.Profile != "" && !config.Name.MatchString(id.Profile) {
		return fmt.Errorf("invalid recorded profile selection")
	}
	if id.ProjectDir != "" && (!id.Project || !filepath.IsAbs(id.ProjectDir) || filepath.Clean(id.ProjectDir) != id.ProjectDir) {
		return fmt.Errorf("invalid recorded project directory")
	}
	return nil
}

func Select(home, workspace, profile string, ignoreProject bool, projectDir string, host config.Host) (Identity, error) {
	canonical, err := Identify(workspace, "", true)
	if err != nil {
		return Identity{}, commanderror.New("workspace_unavailable", "Cannot access workspace: "+err.Error(), workspace, err)
	}
	if host == nil {
		host = config.Snapshot()
	}
	selected, err := artifact.Select(home, canonical.Workspace, artifact.Selection{Profile: profile, IgnoreProject: ignoreProject, ProjectDir: projectDir}, nil, host)
	if err != nil {
		var actionable *commanderror.Error
		if !errors.As(err, &actionable) {
			err = commanderror.New("invalid_configuration", "Cannot select session: "+err.Error(), workspace, err)
		}
		return Identity{}, err
	}
	id, err := Identify(canonical.Workspace, selected.Profile, selected.Project)
	if err != nil {
		return id, err
	}
	id.ProjectDir = selected.ProjectDir
	id.Name = ContainerName(id.Workspace, id.Slot)
	return id, nil
}

func (id Identity) Selector() string { return "." + id.Slot }

func IdentifySlot(workspace, selector string) (Identity, error) {
	slot := strings.TrimPrefix(selector, ".")
	if !strings.HasPrefix(selector, ".") {
		return Identity{}, fmt.Errorf("slot must be .profile-<name>, .profile-<name>.project, or .project")
	}
	profile, project, err := ParseSlot(slot)
	if err != nil {
		return Identity{}, err
	}
	id, err := Identify(workspace, profile, project)
	if err != nil {
		return Identity{}, err
	}
	if id.Selector() != selector {
		return Identity{}, fmt.Errorf("use the exact slot suffix: %s", id.Selector())
	}
	return id, nil
}
