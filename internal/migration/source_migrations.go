package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devbox/internal/config"
	"devbox/internal/docker"
)

// The boundary is the harness-aware Go format, not every JSON document an old
// permissive loader could decode. Config normalization mirrors the old config
// migrations; metadata v3/v4 both record complete creation settings. Earlier
// metadata cannot establish that contract without reconstructing missing facts.
func sourceGlobal(data []byte) (oldGlobal, error) {
	var envelope struct {
		Version *int `json:"version"`
	}
	// Decode the envelope with Unmarshal, then strictly decode the complete
	// selected schema below (including duplicate-key and trailing-value checks).
	if err := json.Unmarshal(data, &envelope); err != nil {
		return oldGlobal{}, fmt.Errorf("invalid global.json: %s", schemaDiagnostic(err))
	}
	var raw map[string]json.RawMessage
	if err := config.Decode(data, &raw); err != nil {
		return oldGlobal{}, fmt.Errorf("invalid global.json: %s", schemaDiagnostic(err))
	}
	version := 1
	if envelope.Version != nil {
		version = *envelope.Version
	} else if string(raw["harnesses"]) != "" && string(raw["harnesses"]) != "null" {
		version = 2
	}
	if version != 1 && version != 2 {
		return oldGlobal{}, fmt.Errorf("global.json: field \"version\": found %d; supported versions are 1 and 2", version)
	}
	g := oldGlobal{Version: 2, ProxyEnabled: true}
	if version == 2 {
		if err := config.Decode(data, &g); err != nil {
			return g, fmt.Errorf("invalid global.json: %s", schemaDiagnostic(err))
		}
	} else {
		type auth struct {
			File string `json:"auth_file"`
		}
		var old struct {
			Version        int      `json:"version"`
			DefaultProfile string   `json:"default_profile"`
			DefaultHarness string   `json:"default_harness"`
			Env            []string `json:"global_env"`
			Proxy          bool     `json:"proxy_enabled"`
			Ignore         bool     `json:"ignore_project_overrides"`
			Pi             auth     `json:"pi"`
			OpenCode       auth     `json:"opencode"`
			Codex          auth     `json:"codex"`
			Copilot        auth     `json:"copilot"`
			Claude         struct {
				File       string `json:"auth_file"`
				Onboarding string `json:"onboarding_file"`
			} `json:"claude"`
		}
		old.Proxy = true
		if err := config.Decode(data, &old); err != nil {
			return g, fmt.Errorf("invalid global.json: %s", schemaDiagnostic(err))
		}
		g.DefaultProfile, g.DefaultHarness, g.GlobalEnv = old.DefaultProfile, old.DefaultHarness, old.Env
		g.ProxyEnabled, g.IgnoreProject = old.Proxy, old.Ignore
		g.Harnesses = map[string]map[string]string{
			"pi": {"auth_file": old.Pi.File}, "opencode": {"auth_file": old.OpenCode.File},
			"claude": {"auth_file": old.Claude.File, "onboarding_file": old.Claude.Onboarding},
			"codex":  {"auth_file": old.Codex.File}, "copilot": {"auth_file": old.Copilot.File},
		}
	}
	g.Version = 2
	return g, nil
}

// Omitted/null versions are permitted in the old sparse layer loader. Explicit
// invalid versions remain errors. Presence of all other fields is retained so
// conversion does not turn inheritance into an explicit default.
func sourceLayer(data []byte) (oldLayer, []string, error) {
	var old oldLayer
	if err := config.Decode(data, &old); err != nil {
		return old, nil, fmt.Errorf("invalid config.json: %s", schemaDiagnostic(err))
	}
	if presence := fieldPresence(data, "version"); presence != "missing" && presence != "null" {
		if problem := versionDiagnostic(data, "version", old.Version, 1); problem != "" {
			return old, nil, fmt.Errorf("config.json: %s", problem)
		}
	}
	old.Version = 1
	changes := []string{}
	hasFlatProxy := fieldPresence(data, "proxy_enabled") != "missing" || fieldPresence(data, "proxy_allowed_domains") != "missing"
	if fieldPresence(data, "proxy") != "missing" && hasFlatProxy {
		return old, nil, fmt.Errorf("config.json mixes flat and nested proxy settings; resolve the conflicting formats before import")
	}
	if old.Proxy != nil || hasFlatProxy {
		changes = append(changes, "Proxy settings removed; Neo does not enforce an allowlist or restrict egress.")
	}
	if old.AutoRebuild != nil {
		changes = append(changes, "Removed auto_rebuild; Neo builds/recreates environments explicitly, not through the old automatic-rebuild setting.")
	}
	return old, changes, nil
}

func metadataProblems(data []byte, m oldMetadata) []string {
	problems := []string{}
	if m.Version != 3 && m.Version != 4 {
		problems = append(problems, fmt.Sprintf("metadata.json: field \"metadata_version\": found %s; supported versions are 3 and 4 with recorded creation settings. Recreate older environments with the old CLI before import.", versionValue(data, "metadata_version", m.Version)))
	}
	if m.Ownership != 0 && m.Ownership != 1 {
		problems = append(problems, fmt.Sprintf("metadata.json: field \"ownership_version\": found %d; supported versions are 0 (pre-label) and 1.", m.Ownership))
	}
	if m.Creation == nil {
		problems = append(problems, "metadata.json: creation_settings is "+fieldPresence(data, "creation_settings")+"; expected a recorded settings object.")
	}
	return problems
}

func versionValue(data []byte, field string, value int) string {
	if p := fieldPresence(data, field); p == "missing" || p == "null" {
		return p
	}
	return fmt.Sprint(value)
}

func (v *Inventory) missingRecord(root string, m oldMetadata) (oldRecord, error) {
	created := m.Created
	if created == "" {
		info, err := os.Stat(root)
		if err != nil {
			return oldRecord{}, err
		}
		created = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	// There is no source identity to preserve. A domain-separated proposed ID
	// remains stable across discovery, locked snapshots, and interrupted staging
	// without initializing session.json in the source installation.
	salt := v.Installation
	if salt == "" {
		salt = v.Paths.Source
	}
	id := digest([]byte("devbox-neo/import-record\x00" + salt + "\x00" + m.Name))[:32]
	return oldRecord{Version: 1, ID: id, Created: created}, nil
}

// The same authority is used for idle checks and container-only config capture.
// Labels win over legacy metadata; partial or foreign ownership labels never
// fall through to pre-label handling. Matching metadata is checked against the
// reviewed source hash before it can authorize reading an unlabeled container.
func verifySourceContainer(ctx context.Context, v *Inventory, item Item, c docker.Container, proxy bool) error {
	labels := c.Config.Labels
	if labels["devbox.managed"] == "true" {
		if v.Installation != "" && labels["devbox.installation_id"] == v.Installation && labels["devbox.session_id"] == item.SessionID {
			return nil
		}
		return fmt.Errorf("source Docker ownership mismatch: %s", item.Name)
	}
	for key := range labels {
		if strings.HasPrefix(key, "devbox.") || strings.HasPrefix(key, "devbox-rewrite.") {
			return fmt.Errorf("source Docker ownership mismatch: %s", item.Name)
		}
	}
	path := filepath.Join(item.Path, "metadata.json")
	data, err := readRegular(ctx, path)
	var meta oldMetadata
	if err != nil || digest(data) != v.SourceHashes[path] || config.Decode(data, &meta) != nil || len(metadataProblems(data, meta)) != 0 || meta.Name != item.Name || meta.Ownership != 0 || (proxy && !meta.ProxyEnabled) {
		return fmt.Errorf("source Docker ownership cannot be verified from legacy metadata: %s", item.Name)
	}
	return nil
}
