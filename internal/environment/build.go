package environment

import (
	"fmt"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/harness"
)

type ImageBuildPlan struct {
	Mode       string
	Source     string
	Dockerfile []byte
	Context    map[string]artifact.ContextFile
	Runtime    []byte
	Arguments  map[string]string
	Ignore     []byte
}

func PlanImage(winners map[string]string, d harness.Definition, uid, gid int) (ImageBuildPlan, error) {
	plan := ImageBuildPlan{Mode: "default", Runtime: ImageDockerfile(d, uid, gid)}
	source := winners["Dockerfile"]
	if source != "" {
		plan.Mode = "normal"
	}
	if source == "" {
		return plan, nil
	}
	plan.Source = source
	captured, err := artifact.ReadBuildContext(source)
	if err != nil {
		return plan, err
	}
	plan.Dockerfile = captured.Dockerfile
	plan.Context = captured.Files
	plan.Ignore = captured.Ignore
	plan.Arguments = map[string]string{"HOST_UID": fmt.Sprint(uid), "HOST_GID": fmt.Sprint(gid)}
	return plan, nil
}
func (p ImageBuildPlan) InputFingerprint() string {
	// Source location is provenance. Temporary directories and session tags must
	// not make identical content appear different across sessions or invocations.
	return Digest(struct {
		Mode                        string
		Dockerfile, Runtime, Ignore []byte
		Context                     map[string]artifact.ContextFile
		Arguments                   map[string]string
	}{p.Mode, p.Dockerfile, p.Runtime, p.Ignore, p.Context, p.Arguments})
}
func (p ImageBuildPlan) FinalDockerfile(base string) []byte {
	if p.Mode == "normal" {
		return []byte(strings.Replace(string(p.Runtime), "FROM debian:bookworm-slim\n", "FROM "+base+"\n", 1))
	}
	return append([]byte(nil), p.Runtime...)
}
