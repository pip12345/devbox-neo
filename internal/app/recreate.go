package app

import (
	"fmt"
	"slices"

	"devbox/internal/artifact"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// recreationSpec changes only the prospective source binding. The old record
// remains the recovery authority until ordinary recreation commits the new one.
func (e *Engine) recreationSpec(old store.Record, q Request) (environment.Spec, error) {
	if q.Recorded != nil && q.Profile != "" && q.Profile != old.Identity.Profile {
		return environment.Spec{}, fmt.Errorf("profile does not match the recorded session")
	}
	if (e.IgnoreProject || q.IgnoreProject) && old.Identity.Project {
		return environment.Spec{}, fmt.Errorf("project exclusion does not match the recorded session")
	}
	identity := old.Identity
	q.Workspace, q.Profile = identity.Workspace, identity.Profile
	q.Sources = slices.Clone(old.Sources)
	if q.ProjectDir != "" {
		if !identity.Project {
			return environment.Spec{}, fmt.Errorf("--project-dir requires an environment with project configuration")
		}
		root, override, err := artifact.ProjectLocation(identity.Workspace, q.ProjectDir)
		if err != nil {
			return environment.Spec{}, err
		}
		found := false
		for i := range q.Sources {
			if q.Sources[i].Label != "project" {
				continue
			}
			if found {
				return environment.Spec{}, fmt.Errorf("recorded project source is ambiguous")
			}
			q.Sources[i].Path = root
			found = true
		}
		if !found {
			return environment.Spec{}, fmt.Errorf("recorded project source is missing")
		}
		identity.ProjectDir = override
	}
	q.Recorded = &identity
	return e.Resolve(q)
}
