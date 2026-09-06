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
	b, refs, err := Expand([]byte(`{"harness_args":["prefix ${env:VALUE}","${env:NEXT}","${env:EMPTY}"],"${env:KEY}":"literal"}`), "config.json", host)
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
func TestSourceReferencesVerifyExpressionAndValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	original := `{"version":1,"extra_env":["TOKEN=${env:TOKEN}"],"harness":"pi"}`
	os.WriteFile(p, []byte(original), 0600)
	l, err := ReadLayer(p, false, Host{"TOKEN": "sentinel-secret"})
	if err != nil {
		t.Fatal(err)
	}
	source := l.EnvInputs[0].Seal("installation")
	encoded, _ := json.Marshal(source)
	if strings.Contains(string(encoded), "sentinel-secret") || strings.Contains(string(encoded), "${env:") {
		t.Fatal("reference stored secret data")
	}
	value, err := source.Restore("installation", Host{"TOKEN": "sentinel-secret"}, map[string][]byte{})
	if err != nil || value != "TOKEN=sentinel-secret" {
		t.Fatal(value, err)
	}
	if _, err = source.Restore("installation", Host{"TOKEN": "changed"}, map[string][]byte{}); err == nil {
		t.Fatal("changed secret accepted for recovery")
	}
	os.WriteFile(p, []byte(strings.Replace(original, `"harness":"pi"`, `"harness":"opencode"`, 1)), 0600)
	if _, err = source.Restore("installation", Host{"TOKEN": "sentinel-secret"}, map[string][]byte{}); err != nil {
		t.Fatal("unrelated source field blocked exact env recovery", err)
	}
	os.WriteFile(p, []byte(strings.Replace(original, "${env:TOKEN}", "sentinel-secret", 1)), 0600)
	if _, err = source.Restore("installation", Host{}, map[string][]byte{}); err == nil {
		t.Fatal("changed expression silently adopted")
	}
}
func TestGlobalPassthroughAndSensitiveValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"version":1,"global_env":["PRESENT","ABSENT","EMPTY","LITERAL=x"]}`), 0600)
	g, err := ReadGlobal(p, Host{"PRESENT": "value", "EMPTY": ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.EnvInputs) != 3 || g.GlobalEnv[0] != "PRESENT=value" || g.GlobalEnv[1] != "EMPTY=" {
		t.Fatal(g.GlobalEnv)
	}
	for _, entry := range []string{"DEVBOX_HOST=value", "BAD-NAME=secret", "KEY=multi\nline", "KEY"} {
		if err = ValidateEnvAssignment(entry); err == nil {
			t.Fatal("invalid env accepted")
		}
	}
}
