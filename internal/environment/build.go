package environment

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/harness"
)

// Image-only mount ancestors must be writable before Docker binds deeper paths.
func mountParentCommand(d harness.Definition) []string {
	args := []string{"/bin/sh", "-eu", "-c", `for dir do
 mkdir -p -- "$dir"
 if [ ! -w "$dir" ] || [ ! -x "$dir" ]; then
  printf 'mount parent is not writable/searchable by devuser: %s\n' "$dir"
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

type ImageStage struct {
	Source       string
	Dockerfile   []byte
	Context      map[string]artifact.ContextFile
	Ignore       []byte
	IgnoreSource string
}

type ImageBuildPlan struct {
	BaseImage      string
	Prepared       []byte
	Stages         []ImageStage
	Boundary       []byte
	Runtime        []byte
	InstallContext map[string]artifact.ContextFile
	Arguments      map[string]string
}

func PlanImage(paths []string, base string, h harness.Effective, uid, gid int) (ImageBuildPlan, error) {
	if !config.ImageReference.MatchString(base) {
		return ImageBuildPlan{}, fmt.Errorf("invalid base_image reference")
	}
	d := h.Definition
	plan := ImageBuildPlan{BaseImage: base, Prepared: preparedDockerfile(base, uid, gid), Boundary: boundaryDockerfile(uid, gid), Runtime: runtimeDockerfile(d, uid, gid),
		Arguments: map[string]string{"DEVBOX_USER": "devuser", "DEVBOX_USER_HOME": "/home/devuser", "DEVBOX_WORKSPACE": "/workspace", "DEVBOX_UID": fmt.Sprint(uid), "DEVBOX_GID": fmt.Sprint(gid)}}
	if d.Install.Script != "" {
		if _, ok := h.InstallFiles[d.Install.Script]; !ok {
			return plan, fmt.Errorf("install.script must name a captured installation file")
		}
		plan.InstallContext = map[string]artifact.ContextFile{"harness-install": {Directory: true, Mode: 0700}}
		for name, file := range h.InstallFiles {
			plan.InstallContext[filepath.Join("harness-install", name)] = artifact.ContextFile{Data: file.Data, Mode: file.Mode}
		}
	}
	for _, source := range paths {
		captured, err := artifact.ReadBuildContext(source)
		if err != nil {
			return plan, err
		}
		stage := ImageStage{Source: source, Dockerfile: captured.Dockerfile, Context: captured.Files, Ignore: captured.Ignore}
		if captured.IgnoreName != "" {
			stage.IgnoreSource = filepath.Join(filepath.Dir(source), captured.IgnoreName)
		}
		plan.Stages = append(plan.Stages, stage)
	}
	return plan, nil
}

func preparedDockerfile(base string, uid, gid int) []byte {
	// Login logs index records by UID; Docker layers can expand their sparse gaps.
	// Skip initialization so high host UIDs cannot inflate the image or exhaust disk.
	return []byte(fmt.Sprintf(`FROM %s
USER root
SHELL ["/bin/sh", "-c"]
RUN . /etc/os-release && case "$ID" in debian|ubuntu) ;; *) echo "Devbox requires a Debian/Ubuntu-compatible base" >&2; exit 1 ;; esac
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl git sudo procps vim zip unzip jq net-tools iputils-ping openssh-client util-linux && rm -rf /var/lib/apt/lists/*
RUN echo "alias ll='ls -alF'" >> /etc/bash.bashrc && echo "alias vi='vim'" >> /etc/bash.bashrc
RUN if id devuser >/dev/null 2>&1; then test "$(id -u devuser)" = %d && test "$(id -g devuser)" = %d && test "$(getent passwd devuser | cut -d: -f6)" = /home/devuser; else if getent passwd %d >/dev/null; then echo "Base image already owns the requested development UID; supply a compatible base without that account" >&2; exit 1; fi; (getent group %d >/dev/null || groupadd -g %d devuser) && useradd --no-log-init -m -s /bin/bash -u %d -g %d devuser; fi
RUN echo 'devuser ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/devuser && chmod 0440 /etc/sudoers.d/devuser
USER devuser
ENV HOME=/home/devuser USER=devuser PATH="/home/devuser/.local/bin:${PATH}:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
WORKDIR /workspace
RUN test -w "$HOME" && sudo -n true
`, base, uid, gid, uid, gid, gid, uid, gid))
}

// A user Dockerfile may temporarily switch USER or WORKDIR. Restore the shared
// build contract between sources without replacing its PATH or filesystem edits.
func boundaryDockerfile(uid, gid int) []byte {
	// Every boundary and final stage receives the preceding image as a build arg.
	// Docker's default-only check cannot see the value supplied by the builder.
	return []byte(fmt.Sprintf(`# check=skip=InvalidDefaultArgInFrom
ARG DEVBOX_BASE
FROM ${DEVBOX_BASE}
USER root
SHELL ["/bin/sh", "-c"]
RUN test "$(id -u devuser)" = %d && test "$(id -g devuser)" = %d && test "$(getent passwd devuser | cut -d: -f6)" = /home/devuser
USER devuser
ENV HOME=/home/devuser USER=devuser
WORKDIR /workspace
RUN test -w "$HOME" && sudo -n true && command -v bash && command -v flock && command -v ssh
`, uid, gid))
}

func runtimeDockerfile(d harness.Definition, uid, gid int) []byte {
	text := string(boundaryDockerfile(uid, gid))
	// Installation must not inherit runtime cache/prefix overrides pointing at
	// bind mounts; executables belong to the image rather than empty state roots.
	if d.Install.Script != "" {
		text += "COPY --chown=devuser:devuser [\"harness-install/\", \"/tmp/devbox-harness-install/\"]\n"
		encoded, _ := json.Marshal([]string{"/bin/bash", "-o", "pipefail", "/tmp/devbox-harness-install/" + d.Install.Script})
		text += "RUN " + string(encoded) + "\n"
		text += "RUN [\"rm\", \"-rf\", \"/tmp/devbox-harness-install\"]\n"
	} else if d.Install.Shell != "" {
		encoded, _ := json.Marshal([]string{"/bin/bash", "-o", "pipefail", "-c", d.Install.Shell})
		text += "RUN " + string(encoded) + "\n"
	}
	parents, _ := json.Marshal(mountParentCommand(d))
	text += "RUN " + string(parents) + "\n"
	if len(d.Install.Path) > 0 {
		// Preserve additions made by every user image stage.
		encoded, _ := json.Marshal(strings.Join(d.Install.Path, ":") + ":${PATH}")
		text += "ENV PATH=" + string(encoded) + "\n"
	}
	check, _ := json.Marshal([]string{"/bin/sh", "-eu", "-c", `command -v "$1" >/dev/null`, "check-harness", d.Binary})
	text += "RUN " + string(check) + "\n"
	return []byte(text)
}
