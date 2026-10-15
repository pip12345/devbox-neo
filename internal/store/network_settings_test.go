package store

import (
	"testing"

	"devbox/internal/environment"
)

func TestSecondaryNetworkSettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		networks []string
		valid    bool
	}{
		{"empty", nil, true},
		{"names", []string{"backend", "dev-net_2"}, true},
		{"duplicate", []string{"backend", "backend"}, false},
		{"empty-name", []string{""}, false},
		{"invalid-name", []string{"bad/name"}, false},
		{"default", []string{"default"}, false},
		{"host", []string{"host"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Settings{Binding: environment.Binding{Workspace: t.TempDir(), LocalName: "test"}, SecondaryNetworks: tc.networks}
			if err := s.Validate(); (err == nil) != tc.valid {
				t.Fatal("unexpected validation result", err)
			}
		})
	}
}
