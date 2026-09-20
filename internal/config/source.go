package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Source records a selected directory and its diagnostic label. The composition
// engine does not derive environment identity from configuration contents.
type Source struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

var ImageReference = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`)

func (s Source) Validate() error {
	if s.Label == "" || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return fmt.Errorf("invalid configuration source")
	}
	return nil
}

// SelectionDefaults deliberately ignores unrelated settings and host references.
func SelectionDefaults(home, profile string, ignore bool, host Host) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if os.IsNotExist(err) {
		return profile, ignore, nil
	}
	if err != nil {
		return "", false, err
	}
	var values map[string]json.RawMessage
	if err = Decode(data, &values); err != nil {
		return "", false, err
	}
	if profile == "" && values["default_profile"] != nil {
		if err = json.Unmarshal(values["default_profile"], &profile); err != nil {
			return "", false, err
		}
		profile, _, err = ExpandString(profile, host)
		if err != nil {
			return "", false, err
		}
	}
	var globalIgnore bool
	if raw := values["ignore_project"]; raw != nil {
		if err = json.Unmarshal(raw, &globalIgnore); err != nil {
			return "", false, err
		}
	}
	return profile, ignore || globalIgnore, nil
}

// SourceMetadata reads only selection inputs. An excluded source's settings,
// host references and artifacts must not prevent another source from opting out.
func SourceMetadata(source Source, proposed *Layer) (bool, error) {
	var values map[string]json.RawMessage
	if proposed != nil {
		data, err := json.Marshal(proposed)
		if err != nil {
			return false, err
		}
		if proposed.Raw != nil {
			data = proposed.Raw
		}
		if err := Decode(data, &values); err != nil {
			return false, err
		}
	} else {
		data, err := os.ReadFile(filepath.Join(source.Path, "config.json"))
		if err != nil {
			return false, err
		}
		if err := Decode(data, &values); err != nil {
			return false, err
		}
	}
	inherit := true
	if raw := values["inherit"]; raw != nil {
		if err := json.Unmarshal(raw, &inherit); err != nil {
			return false, err
		}
	}
	return inherit, nil
}
