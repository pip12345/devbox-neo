package config

import (
	"errors"
	"strings"
	"testing"

	"devbox/internal/commanderror"
)

func TestMissingHarnessIsTypedWithoutInventingAnOwner(t *testing.T) {
	err := Defaults().Validate()
	var actionable *commanderror.Error
	if !errors.As(err, &actionable) || actionable.Code != "harness_required" || len(actionable.Next) != 0 || strings.Contains(err.Error(), "devbox-neo") {
		t.Fatalf("unexpected guidance: %v", err)
	}
}
