package docker

import (
	"fmt"
	"strings"

	"devbox/internal/config"
)

// ValidateEnv defines the currently supported env-file transport. Multiline
// values need a different transport; never fall back to exposing them in argv.
func ValidateEnv(entries []string) error {
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !config.EnvName.MatchString(key) {
			return fmt.Errorf("invalid container environment entry")
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("container env %s: multiline/NUL values are not supported by the current env-file transport", key)
		}
	}
	return nil
}
