package cli

import (
	"devbox/internal/resource"
)

// createConfig is the interactive workflow used by both the standalone command
// and config selection. Only the destination may be supplied by the caller;
// setup, validation, publication and partial results have one implementation.
func createConfig(m menu, s *resource.Service, input, cwd, userHome string) (resource.Owner, resource.Result, bool, error) {
	var owner resource.Owner
	var result resource.Result
	locate := func(value string) error {
		var err error
		owner, err = s.ConfigDirectory(value, cwd, userHome)
		if err == nil {
			err = s.CheckConfigCreation(owner)
		}
		return err
	}
	if input == "" {
		if err := writeMenuTitle(m.Out, "Create config"); err != nil {
			return owner, result, false, err
		}
		_, accepted, err := m.Text("Config name or directory (:back cancels): ", locate)
		if err != nil || !accepted {
			return owner, result, false, err
		}
	} else if err := locate(input); err != nil {
		return owner, result, false, err
	}
	options, proceed, err := configCreationMenu(m, s.Home)
	if err != nil || !proceed {
		return owner, result, false, err
	}
	if err := m.Pause(); err != nil {
		return owner, result, false, err
	}
	result, err = s.CreateConfig(m.Context, owner, options)
	return owner, result, err == nil, err
}
