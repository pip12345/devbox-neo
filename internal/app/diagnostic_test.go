package app

import (
	"bytes"
	"reflect"
	"testing"

	"devbox/internal/docker"
)

func TestDiagnoseCollectsBeforeSynchronousDelivery(t *testing.T) {
	for _, deliver := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-callback", true: "callback"}[deliver], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			e := Engine{Streams: docker.Streams{Out: &stdout, Err: &stderr}}
			first := Diagnostic{Code: "existing"}
			next := Diagnostic{Code: "runtime_deferred", Message: "deferred", Command: []string{"devbox-neo", "stop", "session"}}
			result := Result{Diagnostics: []Diagnostic{first}}
			want := []Diagnostic{first, next}
			calls := 0
			if deliver {
				e.OnDiagnostic = func(got Diagnostic) {
					calls++
					if !reflect.DeepEqual(got, next) || !reflect.DeepEqual(result.Diagnostics, want) {
						t.Fatal("delivery preceded collection or changed the diagnostic", got, result)
					}
				}
			}
			e.diagnose(&result, next)
			if !reflect.DeepEqual(result.Diagnostics, want) || (deliver && calls != 1) {
				t.Fatal("lost collection or delayed/duplicated delivery", result, calls)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatal("app rendered typed diagnostics without a renderer")
			}
		})
	}
}
