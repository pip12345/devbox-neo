package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpansionUsesDecodedStringsAndDoesNotRecurse(t *testing.T) {
	host := Host{"VALUE": "quotes \" slash \\ and newline\n", "NEXT": "${env:MISSING}", "EMPTY": ""}
	b, refs, err := Expand([]byte(`{"harness":"pi","harness_args":["prefix ${env:VALUE}","${env:NEXT}","${env:EMPTY}"],"${env:KEY}":"literal"}`), "config.json", host)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	argv := value["harness_args"].([]any)
	if argv[0] != "prefix "+host["VALUE"] || argv[1] != "${env:MISSING}" || argv[2] != "" || value["${env:KEY}"] != "literal" || len(refs) != 3 {
		t.Fatal("expansion changed JSON structure, keys, or replacement semantics")
	}
	for _, expression := range []string{"${env:MISSING}", "${env:}", "${env:VALUE:-default}", "${env:VALUE"} {
		if _, _, err = ExpandString(expression, host); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
}
func TestLayerResolutionUsesCurrentEnvironmentValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"version":1,"env":["TOKEN=${env:TOKEN}"],"harness":"pi"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"sentinel-secret", "changed"} {
		l, err := ReadLayer(p, Host{"TOKEN": value})
		if err != nil || l.Env[0] != "TOKEN="+value {
			t.Fatal("current environment was not resolved", err)
		}
		if strings.Contains(string(l.Raw), value) {
			t.Fatal("resolution replaced source expressions with expanded values")
		}
	}
}
func TestExplicitEnvironmentReferencesAndSensitiveValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"version":1,"env":["PRESENT=${env:PRESENT}","EMPTY=${env:EMPTY}","LITERAL=x"]}`), 0600)
	g, err := ReadLayer(p, Host{"PRESENT": "value", "EMPTY": ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Env) != 3 || g.Env[0] != "PRESENT=value" || g.Env[1] != "EMPTY=" {
		t.Fatal(g.Env)
	}
	for _, entry := range []string{"DEVBOX_HOST=value", "BAD-NAME=secret", "KEY=multi\nline", "KEY"} {
		if err = ValidateEnvAssignment(entry); err == nil {
			t.Fatal("invalid env accepted")
		}
	}
}
