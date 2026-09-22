package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// EnvSource is a recovery reference, never a saved environment value. File
// references verify both the original expression and its current expansion.
type EnvSource struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Field     string `json:"field,omitempty"`
	Index     int    `json:"index,omitempty"`
	RawHash   string `json:"raw_hash,omitempty"`
	ValueHash string `json:"value_hash"`
	Raw       string `json:"-"`
}
type EnvInput struct {
	Value  string    `json:"-"`
	Source EnvSource `json:"source"`
}

func envHash(key, value string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func (i EnvInput) Seal(key string) EnvSource {
	source := i.Source
	source.RawHash = envHash(key, source.Raw)
	source.ValueHash = envHash(key, i.Value)
	source.Raw = ""
	return source
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
func envInputs(raw, expanded []string, path string) ([]EnvInput, error) {
	result := []EnvInput{}
	for i, value := range expanded {
		if err := ValidateEnvAssignment(value); err != nil {
			return nil, fmt.Errorf("%s env/%d: %w", path, i, err)
		}
		result = append(result, EnvInput{Value: value, Source: EnvSource{Kind: "file", Path: path, Field: "env", Index: i, Raw: raw[i]}})
	}
	return result, nil
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
func (s EnvSource) Restore(key string, host Host, files map[string][]byte) (string, error) {
	if s.Kind != "file" || s.Field != "env" {
		return "", fmt.Errorf("invalid recorded environment source")
	}
	b, ok := files[s.Path]
	if !ok {
		var err error
		b, err = os.ReadFile(s.Path)
		if err != nil {
			return "", fmt.Errorf("recorded env source is unavailable")
		}
		files[s.Path] = b
	}
	var root map[string]json.RawMessage
	if err := Decode(b, &root); err != nil {
		return "", fmt.Errorf("recorded env source is invalid")
	}
	var entries []string
	if err := json.Unmarshal(root[s.Field], &entries); err != nil || s.Index < 0 || s.Index >= len(entries) {
		return "", fmt.Errorf("recorded env entry is unavailable")
	}
	raw := entries[s.Index]
	if envHash(key, raw) != s.RawHash {
		return "", fmt.Errorf("recorded env expression changed")
	}
	value, _, err := ExpandString(raw, host)
	if err != nil {
		return "", err
	}
	if envHash(key, value) != s.ValueHash {
		return "", fmt.Errorf("recorded environment value changed")
	}
	if err = ValidateEnvAssignment(value); err != nil {
		return "", err
	}
	return value, nil
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
