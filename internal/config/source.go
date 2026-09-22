package config

import (
	"fmt"
	"regexp"
)

// Source is an absolute directory input at the composition boundary. Saved
// sessions retain References, so portability is not inferred from this path.
type Source struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

var ImageReference = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`)

func (s Source) Validate() error {
	if s.Label == "" || !cleanAbsolute(s.Path) {
		return fmt.Errorf("invalid configuration source")
	}
	return nil
}
