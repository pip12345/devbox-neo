package environment

import (
	"fmt"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/harness"
)

// The final runtime layer runs this as devuser, before Docker can create
// root-owned mount ancestors. Existing incompatible base-image permissions
// fail at build time; preparation never recursively changes user content.
func mountParentCommand(d harness.Definition) []string {
	args := []string{"/bin/sh", "-eu", "-c", `for dir do
 mkdir -p -- "$dir"
 if [ ! -w "$dir" ] || [ ! -x "$dir" ]; then
  printf 'mount parent is not writable/searchable by devuser: %s\n' "$dir" >&2
  exit 1
 fi
done`, "mount-parents"}
	for _, parent := range d.MountParents() {
		if parent.Mount == "" {
			args = append(args, parent.Target)
		}
	}
	return args
}

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
