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
separate from saved session state. Session copy and copy --move transfer only declared
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

Do not modify /devbox: it is Devbox-owned runtime data. Do not add a Dockerfile
to a selected config directory to persist an ad-hoc installation without approval.
Devbox does not enforce network egress restrictions or provide an offline mode.
On the host, config create <reference> creates a config directory; config edit
edits that directory or adds missing optional files. Bare config names use the
selected home (normally ~/.devbox-neo/configs/); directories elsewhere are ordinary
explicit sources too. Creation rejects existing config.json; artifact setup never
overwrites existing files. Harness-file generation does not force harness selection.
Devbox-neo create <folder> asks for an explicit local session name and existing
config sources, then prepares a stopped container. Scripts supply --name NAME and
repeated --config REF. Creation never selects a default. Use set <folder> to choose
one, open <folder> --name NAME for an explicit local name, or a full session name
from list for exact targeting anywhere. Even a sole session needs an explicit
folder default. Config sources <folder|session> manages saved source references;
it does not open directory editors. Config edits do not rename sessions.
Open/start require existing sessions, with recorded missing-container recovery.
Lasting settings belong in config directories; open accepts continuation and
invocation-only harness arguments.
Manual start keeps a container running until stop, including automatic restart
when Docker starts after reboot. Without manual start, the last attached Devbox
command stops it. Open never changes this intent. Reboot restarts the container,
not the prior harness process, terminal, or SSH connection.
On the host, devbox-neo list shows saved environments and their running/stopped/missing
containers. Status --all checks their configuration and changed local inputs, including
environments without containers; it does not check upstream releases. Both commands
warn separately about managed containers without session records. Status <folder|session>
combines saved session details, active commands, container state, and pending changes;
invalid current config does not hide saved details. Copy is also a top-level command;
add --move to remove the source after the destination is ready. There is no separate
show command or session command group.
Managed config-source files are authoritative: local edits to their live copies
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
Session record schema 5 stores desired relative/fixed source references separately
from complete applied inputs and committed source directories. Older development
records require a clean reset with the previous build; runtime has no migration reader.
Copy --as NAME chooses a destination local name, including in the same folder.
Relative source references follow the destination workspace; fixed references stay
absolute. Config directories are not copied. Whole-session deletion and move cleanup
clear a matching source default without selecting a replacement. Container-only
deletion leaves defaults intact. Session lookup and source-chain repair do not require working config directories.

The built-in Pi launch defaults to --tui-mode fullscreen (upstream experimental).
A later --tui-mode regular in harness_args or one-off harness arguments overrides it.
Configured harness_args must name their harness in the same config file; arguments
from layers naming a different harness are ignored. Configuration fields include
mounts, ports, env, shell, network, and base_image. Built-in defaults are followed
by every explicitly selected config source in order. Scalars replace, most lists
append, shell argv replaces as a unit, and overlapping mount targets fail. No global
settings, profile/project discovery, or inheritance cutoff participates. Source edits
can save incomplete chains for repair; creation, open (even while running), and
recreation require at least one valid source and a final harness selection.

base_image chooses the upstream Debian/Ubuntu-compatible image. Devbox prepares
the development user/runtime before custom Dockerfiles build in source order.
Each Dockerfile extends DEVBOX_BASE and keeps its own context and ignore rules;
use sudo for system installation and normal devuser execution for user tools.
DEVBOX_USER, DEVBOX_USER_HOME, DEVBOX_WORKSPACE, DEVBOX_UID and DEVBOX_GID are
available build arguments. Conflicting base accounts fail without being changed.
Devbox restores the build-user contract between stages, preserves custom PATH,
and installs the harness last. Keep tool installation in cached image builds.
setup.sh scripts run in source order per container creation/recreation;
before-open.sh scripts run in source order before each harness launch through
open. Each is a separate process in /workspace; a failure stops the chain.
Existing recorded environments require recreation to adopt changed harness defaults.
Host CLI failures show short messages, target context, and labeled next commands.
Suggestions do not run automatically; Then marks a sequence and Or an alternative.
Missing-session hints retain the folder you entered. No suggestion runs another
command's menu automatically. Full session names are lookup keys, not Docker
ownership proof.
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
