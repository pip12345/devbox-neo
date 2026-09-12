// Package assets owns the in-container runtime documentation contract.
package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"strings"

	"devbox/docs"
)

const AgentGuide = `# Devbox environment

You are inside a persistent Linux development container. /workspace is the host
workspace. Docker and the host devbox-neo CLI normally live outside this container.
Do not try to manage this container or start a Docker daemon inside it.

Read /devbox/docs/index.md for the runtime's documentation. /devbox/network/env
contains shell-safe network facts, and /devbox/network/inspect.json has live facts.
Use DEVBOX_HOST to reach host services; services need to listen on a reachable
interface, not host loopback alone on a bridge network. Listen on 0.0.0.0 for
container services that must be reached outside the container.

Harness config copies skip symlinks and other non-regular entries with host-side
warnings. Skipped extension dependency links are not supplied by that source;
install dependencies in the container if the extension needs them.

The runtime includes vim, zip, unzip, jq, net-tools, and iputils-ping alongside
Bash, git, curl, sudo, and procps. Interactive Bash provides ll='ls -alF' and
vi='vim'. Install other development tools in the container when needed. Container-layer changes
survive stop/start but are lost on recreation. Workspace files and declared
harness stores persist on the host. Devbox prepares parents for declared harness
mounts, but unmounted directories remain container-local even when writable.
Managed authentication and shared caches are
separate from resettable session state. Session clone/relocate copy only declared
harness state, not workspace files or container-layer tools. Pending transfers
reserve both endpoints; retry the same host CLI command instead of deleting
journals or session directories by hand.

Do not modify /devbox: it is Devbox-owned runtime data. Do not add a project
Dockerfile to make an ad-hoc tool installation persistent without user approval.
Devbox does not enforce network egress restrictions or provide an offline mode.
On the host, devbox-neo create <folder> prepares a new environment and leaves it
stopped without launching a harness. Plain open/start require an existing session;
open <folder> --create explicitly creates if missing and then opens. They still
recover a missing container for retained session state without --create.
On the host, devbox-neo status --all shows which existing containers need recreation
or rebuilding from changed local inputs; it does not check upstream releases.
Ordinary recreate builds changed image inputs automatically. Open prints creation
drift reasons first, before startup, then continues immediately. Status and open
share detailed setting/file changes; env reasons show variable names, never values.
Session record schema 2 requires a complete applied-input snapshot. Older development
records require a clean reset with the previous build; there is no automatic migration.

The built-in Pi launch defaults to --tui-mode fullscreen (upstream experimental).
A later --tui-mode regular in harness_args or one-off harness arguments overrides it.
Existing recorded environments require recreation to adopt changed harness defaults.
Host CLI failures include error codes and next commands where safe; suggestions do
not run automatically. Existing --json commands also return structured failures.

Attached shell, harness, and exec commands receive the invoking terminal's TERM,
COLORTERM, and related display variables. These are not durable session settings;
reconnecting refreshes them without recreation. Host shell dotfiles are not imported.
`

func Files() (map[string][]byte, error) {
	files := map[string][]byte{"AGENTS.md": []byte(AgentGuide)}
	err := fs.WalkDir(docs.Files, "src", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := docs.Files.ReadFile(p)
		if err != nil {
			return err
		}
		files["docs/"+strings.TrimPrefix(p, "src/")] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries, err := docs.Files.ReadDir("dev")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		b, err := docs.Files.ReadFile("dev/" + entry.Name())
		if err != nil {
			return nil, err
		}
		files["dev/"+entry.Name()] = b
	}
	return files, nil
}
func Hash() (string, error) {
	files, err := Files()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
