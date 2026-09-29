package harness

import (
	"fmt"
	"os"

	"devbox/internal/config"
)

// Recorded reads exactly the recorded definition source. It does not consult
// user overrides or select a replacement if an embedded/source file has changed.
// The caller must verify the returned digest before using any returned values.
func Recorded(name, origin string) (Definition, string, error) {
	var d Definition
	if !config.Name.MatchString(name) {
		return d, "", fmt.Errorf("invalid recorded harness")
	}
	var b []byte
	var err error
	if origin == "builtin" {
		b, err = builtins.ReadFile("builtin/" + name + "/harness.json")
	} else {
		b, err = os.ReadFile(origin)
	}
	if err != nil {
		return d, "", err
	}
	d, err = parseDefinition(b)
	if err != nil {
		return d, "", fmt.Errorf("recorded definition input is invalid: %w", err)
	}
	install, err := readInstall(d, origin)
	if err != nil {
		return d, "", err
	}
	hash, err := definitionHash(b, install.Files)
	return d, hash, err
}
