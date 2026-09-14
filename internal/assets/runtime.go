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
Bash, git, curl, sudo, procps, OpenSSH client tools, and util-linux (flock). Interactive Bash provides ll='ls -alF' and
vi='vim'. Install other development tools in the container when needed. Container-layer changes
survive stop/start but are lost on recreation. Workspace files and declared
harness stores persist on the host. Devbox prepares parents for declared harness
mounts, but unmounted directories remain container-local even when writable.
Managed authentication and shared caches are
separate from saved session state. Session clone/relocate copy only declared
harness state, not workspace files or container-layer tools. Pending transfers
reserve both endpoints; retry the same host CLI command instead of deleting
journals or session directories by hand.

Use /devbox/ssh/config for user-supplied SSH connections. Read the config and its
included /devbox/ssh/c/*/config files for available aliases, then use ordinary
ssh or scp with -F /devbox/ssh/config. Never handle interactive SSH authentication,
search for host credentials, or bypass an unavailable shared connection with a
fresh login. Ask the user to run devbox-neo ssh <environment> <destination> in a
host terminal; use this environment's exact name from /devbox/network/inspect.json.
The user authenticates and keeps that terminal open; Ctrl-C ends the connection.
After cleanup the host command restores the terminal and prints Disconnected.
Actual authentication, connection, or cleanup failures still report errors.
The master runs inside the container by default. --host-master is an explicit
user choice that shares a host connection, including host-side forwarding.
Normal SSH configuration controls keys, ProxyJump, agent and X11 forwarding;
Devbox does not copy credentials or force forwarding off. Consult
/devbox/docs/guides/ssh.md for the workflow. Connection access does not authorize
unrelated remote changes or use of host/network services.

Do not modify /devbox: it is Devbox-owned runtime data. Do not add a project
Dockerfile to make an ad-hoc tool installation persistent without user approval.
Devbox does not enforce network egress restrictions or provide an offline mode.
On the host, devbox-neo create <folder> prepares a new environment and leaves it
stopped without launching a harness. Open/start require an existing session and
never create new sessions. They still recover a missing container for retained
session state. Container-setting flags belong to create/recreate; open accepts
launch settings, continuation, and harness arguments.
On the host, devbox-neo list shows saved environments and their running/stopped/missing
containers. Status --all checks their configuration and changed local inputs, including
environments without containers; it does not check upstream releases. Both commands
warn separately about managed containers without session records. Status <target>
combines saved session details, active commands, container state, and pending changes;
invalid current config does not hide saved details. Clone and relocate are also
top-level commands; there is no separate show command or session command group.
Managed profile/project files are authoritative: local edits to their live copies
are overwritten at the next startup. Open/start/shell/exec/ssh synchronize before
starting stopped containers, never merely when attaching to running ones.
Invalid participating config blocks startup. Pi's shared JSON preserves keys not
owned by Devbox; unmanaged files and conversations are not wiped. Creation/recreation
also synchronizes. A changed harness layout requires recreation before its config
can be applied. There is no reset command.
Delete asks about the container first, then saved state/history. Explicit --container
deletes runtime only; --session deletes the whole environment without prompts.
These scope flags are mutually exclusive and required for scripts/dry runs.
Delete owns --older-than, --orphaned, --stopped and --all selection; combined filters
intersect. There is no prune command. --force only permits interrupting attached
container commands; it never expands scope or bypasses saved-state idle checks.
Ordinary recreate builds changed image inputs automatically. Open prints creation
drift reasons first, before startup, then continues immediately. Status and open
share detailed setting/file changes; env reasons show variable names, never values.
Session record schema 2 requires a complete applied-input snapshot. Older development
records require a clean reset with the previous build; there is no automatic migration.

The built-in Pi launch defaults to --tui-mode fullscreen (upstream experimental).
A later --tui-mode regular in harness_args or one-off harness arguments overrides it.
Existing recorded environments require recreation to adopt changed harness defaults.
Host CLI failures show short messages, target context, and labeled next commands.
Suggestions do not run automatically; Then marks a sequence and Or an alternative.
Generic creation hints show only create, without profile flags.
Missing-environment hints retain the folder you entered.
They use normal configuration selection, not a replay of prior flags.
Existing --json commands retain error codes, operation names, and structured failures.

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
