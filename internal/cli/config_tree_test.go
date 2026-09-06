package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func TestResourceWarningsInTextAndJSON(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		var out, stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		result := resource.Result{Path: "/profile/config.json", Warnings: []string{"skipping non-regular config entry"}}
		if err := renderResource(cmd, result, nil, asJSON, ""); err != nil {
			t.Fatal(err)
		}
		if asJSON {
			var decoded resource.Result
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || len(decoded.Warnings) != 1 || stderr.Len() != 0 {
				t.Fatal("JSON must carry warnings without mixing text into output", out.String(), stderr.String(), err)
			}
		} else if !strings.Contains(stderr.String(), "Warning: "+result.Warnings[0]) {
			t.Fatal("text result omitted stderr warning", stderr.String())
		}
	}
}
