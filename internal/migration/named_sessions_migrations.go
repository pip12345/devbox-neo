package migration

import (
	"path/filepath"
	"strings"

	"devbox/internal/config"
)

const importedGlobalName = "imported-global"

func importedGlobalPath(home string) string {
	return filepath.Join(home, "configs", importedGlobalName, "config.json")
}

// Old global settings become an ordinary explicit source. Discovery controls
// choose the import's source list, never destination runtime defaults.
func convertedGlobal(g oldGlobal) config.Layer {
	layer := config.Layer{Version: 1}
	if g.DefaultHarness != "" {
		layer.Harness = &g.DefaultHarness
	}
	for _, entry := range g.GlobalEnv {
		if !strings.Contains(entry, "=") {
			entry += "=${env:" + entry + "}"
		}
		layer.Env = append(layer.Env, entry)
	}
	return layer
}

// Distinct prefixes keep old profile slots separate from the old project slot,
// including an old profile literally named "project". The merge review exposes
// and approves these initial names; later config edits cannot change them.
func importLocalName(item Item, profile string) string {
	if item.Profile == "" {
		return "project"
	}
	return "profile-" + profile
}

func importReferences(home string, item Item, profile string) []config.Reference {
	refs := []config.Reference{{Label: importedGlobalName, Kind: config.ReferenceFixed, Path: filepath.Dir(importedGlobalPath(home))}}
	if profile != "" {
		refs = append(refs, config.Reference{Label: profile, Kind: config.ReferenceFixed, Path: filepath.Join(home, "configs", profile)})
	}
	if item.Project {
		refs = append(refs, config.Reference{Label: "workspace", Kind: config.ReferenceRelative, Path: ".devbox"})
	}
	return refs
}
