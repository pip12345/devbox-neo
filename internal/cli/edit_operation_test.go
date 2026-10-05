package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func TestEditParsesExactlyOneOperation(t *testing.T) {
	operations := []editOperation{editShow, editDefault, editClearDefault, editConfigs, editWorkspace}
	for mask := 0; mask < 32; mask++ {
		var requested [5]bool
		count, expected := 0, editBrowse
		for i := range requested {
			requested[i] = mask&(1<<i) != 0
			if requested[i] {
				count++
				expected = operations[i]
			}
		}
		operation, err := parseEditOperation(requested[0], requested[1], requested[2], requested[3], requested[4])
		if (err != nil) != (count > 1) || err == nil && operation != expected {
			t.Fatalf("mask %d: %q, %v", mask, operation, err)
		}
	}
}

func TestEditOperationRequirements(t *testing.T) {
	for _, operation := range []editOperation{editBrowse, editShow, editDefault, editClearDefault, editConfigs, editWorkspace} {
		for mask := 0; mask < 16; mask++ {
			asJSON, named, direct, terminal := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0
			jsonSupported := operation == editShow || operation == editConfigs || operation == editWorkspace
			needsSession := operation != editBrowse && operation != editClearDefault
			invalid := asJSON && !jsonSupported || operation == editClearDefault && named || needsSession && !direct || operation == editBrowse && !terminal
			if err := operation.validate(asJSON, named, direct, terminal); (err != nil) != invalid {
				t.Fatalf("%q mask %d: %v", operation, mask, err)
			}
		}
	}
}

func TestEditFlagPresenceAndValidationBeforeInitialization(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		initialize bool
	}{
		{[]string{"dbx-folder.session", "--show=false"}, false},
		{[]string{"dbx-folder.session", "--config="}, true},
		{[]string{"dbx-folder.session", "--workspace="}, true},
		{[]string{"dbx-folder.session", "--show=false", "--default"}, true},
		{[]string{"dbx-folder.session", "--show", "--default"}, false},
		{[]string{"dbx-folder.session", "--config=", "--workspace="}, false},
		{[]string{".", "--show"}, false},
		{[]string{"dbx-folder.session", "--default", "--json"}, false},
	} {
		called := false
		failure := errors.New("initialized")
		name := ""
		cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { called = true; return nil, failure }, &name)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(tc.args)
		err := cmd.ExecuteContext(context.Background())
		if called != tc.initialize || called && !errors.Is(err, failure) || !called && err == nil {
			t.Fatal(tc.args, called, err)
		}
	}
}
