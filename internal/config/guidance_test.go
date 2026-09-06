package config

import "testing"

func TestMissingHarnessGuidanceSeparatesProfileAndProjectCommands(t *testing.T) {
	err := Defaults().Validate()
	want := "no harness selected.\n\nFor a profile:\n  devbox-neo profile init <name> --harness <harness>\n\nFor a project:\n  devbox-neo project init <folder> --harness <harness>"
	if err == nil || err.Error() != want {
		t.Fatalf("unexpected guidance: %v", err)
	}
}
