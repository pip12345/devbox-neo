package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/config"
)

// Participation is identity, not a snapshot of configuration values. Recorded
// participation pins the sources while their contents remain editable.
type Participation struct {
	Profile string `json:"profile,omitempty"`
	Project bool   `json:"project"`
}

type Selection struct {
	Profile       string
	IgnoreProject bool
	Recorded      *Participation
}

// Select reads only identity-affecting settings. Looking up a session must not
// depend on build inputs, harness definitions, or unrelated host env references.
func Select(home, workspace string, q Selection, proposed *config.Layer, host config.Host) (Participation, error) {
	p := Participation{Profile: q.Profile}
	if q.Recorded != nil {
		p = *q.Recorded
	}
	if q.Recorded == nil {
		values, err := selectionFile(filepath.Join(home, "config.json"))
		if err != nil && !os.IsNotExist(err) {
			return p, err
		}
		if p.Profile == "" && values["default_profile"] != nil {
			if err := json.Unmarshal(values["default_profile"], &p.Profile); err != nil {
				return p, err
			}
			expanded, _, err := config.ExpandString(p.Profile, host)
			if err != nil {
				return p, err
			}
			p.Profile = expanded
		}
		var ignore bool
		if raw := values["ignore_project"]; raw != nil {
			if err := json.Unmarshal(raw, &ignore); err != nil {
				return p, err
			}
		}
		q.IgnoreProject = q.IgnoreProject || ignore
	}
	if workspace != "" && ((q.Recorded == nil && !q.IgnoreProject) || (q.Recorded != nil && p.Project)) {
		file := filepath.Join(workspace, ".devbox", "config.json")
		var inherit *bool
		present := proposed != nil
		if proposed != nil {
			inherit = proposed.InheritProfile
		} else {
			values, err := selectionFile(file)
			if err == nil {
				present = true
				if raw := values["inherit_profile"]; raw != nil {
					if err = json.Unmarshal(raw, &inherit); err != nil {
						return p, err
					}
				}
			} else if !os.IsNotExist(err) {
				return p, err
			} else {
				present, err = hasProjectArtifacts(filepath.Dir(file))
				if err != nil {
					return p, err
				}
			}
		}
		if q.Recorded != nil && !present {
			return p, fmt.Errorf("recorded project configuration is missing: %s", file)
		}
		p.Project = present
		if present && inherit != nil && !*inherit {
			if q.Profile != "" || (q.Recorded != nil && p.Profile != "") {
				return p, fmt.Errorf("project disables profile inheritance; use --ignore-project for a profile-only session")
			}
			p.Profile = ""
		}
	}
	if p.Profile != "" && !config.Name.MatchString(p.Profile) {
		return p, fmt.Errorf("invalid profile name")
	}
	if p.Profile == "" && !p.Project {
		return p, commanderror.New("configuration_missing", "No profile or project configuration selected.", workspace, nil,
			commanderror.Next("Create a profile", "profile", "create", "<profile>"),
			commanderror.Next("Use as default (optional)", "profile", "set", "<profile>"),
			commanderror.Next("Or configure this project", "project", "create", workspace))
	}
	return p, nil
}

func selectionFile(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values map[string]json.RawMessage
	if err = config.Decode(b, &values); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return values, nil
}
