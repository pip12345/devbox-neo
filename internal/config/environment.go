package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Host map[string]string

func Snapshot() Host {
	host := Host{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			host[key] = value
		}
	}
	return host
}

func ExpandString(text string, host Host) (string, []string, error) {
	var result strings.Builder
	names := []string{}
	for {
		start := strings.Index(text, "${env:")
		if start < 0 {
			result.WriteString(text)
			break
		}
		result.WriteString(text[:start])
		text = text[start+6:]
		end := strings.IndexByte(text, '}')
		if end < 0 {
			return "", nil, fmt.Errorf("unterminated host environment reference")
		}
		name := text[:end]
		if !EnvName.MatchString(name) {
			return "", nil, fmt.Errorf("invalid host environment reference name")
		}
		value, present := host[name]
		if !present {
			return "", nil, fmt.Errorf("host environment variable %s is unset", name)
		}
		result.WriteString(value)
		names = append(names, name)
		text = text[end+1:]
	}
	return result.String(), names, nil
}

// Expansion operates on decoded values, never JSON syntax or property names.
// Replacement text is appended once and is not interpreted recursively.
func Expand(b []byte, path string, host Host) ([]byte, map[string][]string, error) {
	var root map[string]any
	if err := Decode(b, &root); err != nil {
		return nil, nil, err
	}
	references := map[string][]string{}
	var walk func(any, string) (any, error)
	walk = func(value any, field string) (any, error) {
		switch value := value.(type) {
		case string:
			expanded, names, err := ExpandString(value, host)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", path, field, err)
			}
			if len(names) > 0 {
				references[field] = names
			}
			return expanded, nil
		case []any:
			for i, item := range value {
				expanded, err := walk(item, fmt.Sprintf("%s/%d", field, i))
				if err != nil {
					return nil, err
				}
				value[i] = expanded
			}
			return value, nil
		case map[string]any:
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				expanded, err := walk(value[key], field+"/"+child)
				if err != nil {
					return nil, err
				}
				value[key] = expanded
			}
			return value, nil
		default:
			return value, nil
		}
	}
	if _, err := walk(root, ""); err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(root)
	return data, references, err
}
func ValidateEnvAssignment(value string) error {
	key, _, ok := strings.Cut(value, "=")
	if !ok || !EnvName.MatchString(key) {
		return fmt.Errorf("expected an environment KEY=VALUE assignment")
	}
	if strings.HasPrefix(key, "DEVBOX_") {
		return fmt.Errorf("DEVBOX_* environment is reserved")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("environment values must be single-line and contain no NUL")
	}
	return nil
}
func RedactEnv(entries []string) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry, "=")
		if !EnvName.MatchString(key) {
			key = "<env>"
		}
		result = append(result, key+"=<redacted>")
	}
	return result
}
