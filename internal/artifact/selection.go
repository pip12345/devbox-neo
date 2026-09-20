package artifact

import (
	"fmt"
	"os"
	"path/filepath"

	"devbox/internal/commanderror"
	"devbox/internal/config"
)

type SourceHeader struct {
	Inherit bool
	Checked bool
}

// SourcePreview binds proposed settings to their source before inheritance can
// remove other sources. Selection and full resolution use the same binding.
type SourcePreview struct {
	Path  string
	Layer config.Layer
}

func (p *SourcePreview) layer(source config.Source) *config.Layer {
	if p != nil && p.Path == source.Path {
		return &p.Layer
	}
	return nil
}

type Participation struct {
	Profile string          `json:"profile,omitempty"`
	Project bool            `json:"project"`
	Sources []config.Source `json:"sources"`
	Headers []SourceHeader  `json:"-"`
	Preview *SourcePreview  `json:"-"`
}

type Selection struct {
	Profile       string
	IgnoreProject bool
	Sources       []config.Source
	Recorded      *Participation
}

// Select owns the profile/project convenience frontend. RetainSources owns the
// generic inheritance rule and receives only ordered directory references.
func Select(home, workspace string, q Selection, proposed *config.Layer, host config.Host) (Participation, error) {
	p := Participation{}
	var sources []config.Source
	if q.Recorded != nil {
		p = *q.Recorded
		sources = append([]config.Source(nil), p.Sources...)
		if len(sources) == 0 {
			return p, fmt.Errorf("recorded configuration sources are missing; a clean development session reset is required")
		}
	} else if q.Sources != nil {
		p.Profile = q.Profile
		sources = append([]config.Source(nil), q.Sources...)
	} else {
		profile, ignore, err := config.SelectionDefaults(home, q.Profile, q.IgnoreProject, host)
		if err != nil {
			return p, err
		}
		p.Profile = profile
		if p.Profile != "" {
			if !config.Name.MatchString(p.Profile) {
				return p, fmt.Errorf("invalid profile name")
			}
			sources = append(sources, config.Source{Label: "profile", Path: filepath.Join(home, "profiles", p.Profile)})
		}
		if workspace != "" && !ignore {
			root := filepath.Join(workspace, ".devbox")
			_, statErr := os.Stat(filepath.Join(root, "config.json"))
			if statErr != nil && !os.IsNotExist(statErr) {
				return p, statErr
			}
			if proposed != nil || statErr == nil {
				p.Project = true
				sources = append(sources, config.Source{Label: "project", Path: root})
			} else {
				present, err := hasProjectArtifacts(root)
				if err != nil {
					return p, err
				}
				if present {
					return p, fmt.Errorf("project artifacts require config.json: %s", root)
				}
			}
		}
	}
	p.Preview = nil
	if proposed != nil {
		for _, source := range sources {
			if source.Label == "project" {
				p.Preview = &SourcePreview{Path: source.Path, Layer: *proposed}
				break
			}
		}
	}
	kept, headers, err := RetainSources(sources, p.Preview)
	if err != nil {
		return p, err
	}
	if len(kept) == 0 {
		return p, commanderror.New("configuration_missing", "No profile or project configuration selected.", workspace, nil,
			commanderror.Next("Create a profile", "profile", "create", "<profile>"),
			commanderror.Next("Use as default (optional)", "profile", "set", "<profile>"),
			commanderror.Next("Or configure this project", "project", "create", workspace))
	}
	p.Sources, p.Headers = kept, headers
	p.Project = false
	profile := false
	for _, source := range kept {
		profile = profile || source.Label == "profile"
		p.Project = p.Project || source.Label == "project"
	}
	if !profile {
		p.Profile = ""
	}
	if q.Recorded != nil && (p.Profile != q.Recorded.Profile || p.Project != q.Recorded.Project || len(p.Sources) != len(q.Recorded.Sources)) {
		return p, fmt.Errorf("inheritance changed the recorded source selection; create a new environment instead of renaming saved state")
	}
	return p, nil
}

// Walking backwards lets a cutoff discard unavailable or invalid earlier
// directories before reading their metadata, settings, or artifacts.
func RetainSources(sources []config.Source, proposed *SourcePreview) ([]config.Source, []SourceHeader, error) {
	start := 0
	headers := make([]SourceHeader, len(sources))
	for i := len(sources) - 1; i >= 0; i-- {
		if err := sources[i].Validate(); err != nil {
			return nil, nil, err
		}
		// The first source has no predecessors. Its inherit value cannot change
		// selection, so malformed settings must not block identity-only lookup.
		if i == 0 {
			break
		}
		inherit, err := config.SourceMetadata(sources[i], proposed.layer(sources[i]))
		if err != nil {
			return nil, nil, fmt.Errorf("%s configuration: %w", sources[i].Path, err)
		}
		headers[i] = SourceHeader{Inherit: inherit, Checked: true}
		if !inherit {
			start = i
			break
		}
	}
	return append([]config.Source(nil), sources[start:]...), headers[start:], nil
}
