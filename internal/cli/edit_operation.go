package cli

import "fmt"

type editOperation string

const (
	editBrowse       editOperation = ""
	editShow         editOperation = "--show"
	editConfigs      editOperation = "--config"
	editWorkspace    editOperation = "--workspace"
	editDefault      editOperation = "--default"
	editClearDefault editOperation = "--clear-default"
)

// Flag presence selects config/workspace edits, including explicit empty values.
// Boolean flags select an operation only when true, not merely when supplied.
func parseEditOperation(show, setDefault, clearDefault, replace, changeWorkspace bool) (editOperation, error) {
	operation := editBrowse
	for _, choice := range []struct {
		operation editOperation
		requested bool
	}{
		{editWorkspace, changeWorkspace},
		{editConfigs, replace},
		{editShow, show},
		{editDefault, setDefault},
		{editClearDefault, clearDefault},
	} {
		if !choice.requested {
			continue
		}
		if operation != editBrowse {
			return editBrowse, fmt.Errorf("choose one edit operation: --show, --config, --workspace, --default, or --clear-default")
		}
		operation = choice.operation
	}
	return operation, nil
}

func (operation editOperation) validate(asJSON, named, direct, terminal bool) error {
	if asJSON && operation != editShow && operation != editConfigs && operation != editWorkspace {
		return fmt.Errorf("--json requires --show, --config, or --workspace")
	}
	if operation == editClearDefault && named {
		return fmt.Errorf("use --name or --clear-default, not both")
	}
	switch operation {
	case editWorkspace, editShow, editDefault, editConfigs:
		if !direct {
			return fmt.Errorf("%s requires --name or a session directory name", operation)
		}
	case editBrowse:
		if !terminal {
			return fmt.Errorf("editing requires a terminal; use --name NAME with --config to replace selected configs, --show to inspect, or --default to select without prompting")
		}
	}
	return nil
}
