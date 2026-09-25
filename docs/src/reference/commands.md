# Commands

Executable: `devbox-neo`. Use `<command> --help` for command-specific help.

## Global options and targets

| Option | Meaning |
|---|---|
| `--home PATH` | Select the Devbox home; overrides `DEVBOX_HOME`, then `~/.devbox-neo` |

`<folder|session>` accepts a workspace folder or the full saved session/container name from `list --wide` or single-target `status`. For single-session operations, a folder plus `--name NAME` selects that local name; a folder alone requires its saved default. Exact full names work from any folder. There is no sole-session fallback or implicit creation. A local name by itself is not a session target.

`config create` and `config edit` address independent config directories, not environments. Bare references use `<home>/configs/`; path references follow the [config reference rules](configuration.md#locations-and-references).

## Environment lifecycle

| Command | Effect |
|---|---|
| `create <folder> [--name NAME] [--config NAME_OR_PATH ...]` | Name a session and select existing configs; build and leave it stopped without selecting a default |
| `open <folder\|session> [-- harness-args...]` | Launch the recorded harness in an existing environment |
| `start <folder\|session>` | Start and keep running until explicit `stop`, including automatic restart when Docker starts after reboot |
| `stop <folder\|session> [--force]` | Stop and clear manual keep-running intent; `--force` permits interrupting attached commands |
| `shell <folder\|session>` | Open the configured shell in `/workspace` |
| `exec <folder\|session> -- <argv...>` | Run exact arguments, without implicit shell interpretation |
| `logs <folder\|session> [-f] [--tail N\|all]` | Read Docker logs; default tail `100`; `-f`/`--follow` streams |
| `recreate <folder\|session> [--image]` | Replace the container using current configuration; preserve session identity and stores |
| `recreate --all [--image]` | Preflight and recreate all Devbox containers |

`open` and `start` can restore a missing container from retained session state when recorded inputs remain available. `shell`, `exec`, and `ssh` require the container to exist. None of these commands creates a new session.

Before a stopped container starts, access commands resolve participating configuration and synchronize compatible managed files. Invalid config or conflicting shared JSON blocks startup. Running access does not synchronize files; running `start`, `shell`, `exec`, and `ssh` also skip desired-config resolution. `open` still resolves launch settings and reports pending creation changes.

`recreate` reuses an available recorded image if image inputs are unchanged; otherwise it builds with cache. `--image` forces a no-cache build. Recreation preserves running/stopped intent but loses container-local changes. Docker logs do not include a transcript of attached `exec` output.

### Creation and launch options

Configure container settings through `config edit <name|path>` before creation/recreation. A terminal opens a creation menu where you can set or change the pending session name and select ordered configs in either order. `--name` and repeated `--config` prefill those choices; **Create session** is always available and reports any missing name or config when selected. Without a terminal, both are required. A fully specified create does not prompt. Session names are explicit and case-sensitive.

| Option | Commands | Meaning |
|---|---|---|
| `--harness-arg ARG` | open | One-off harness argument; repeatable |
| `--continue`, `-c` | open | Append the harness's continuation arguments |
| `-- <args...>` | open | One-off harness arguments, appended last |

Continuation arguments precede one-off arguments. Launch arguments are not saved as overrides.

Without manual `start`, the last attached `open`, `shell`, `exec`, or `ssh` command stops the container. Manual `start`, including while attachments are active, keeps it running until `stop`. Neither new attachments nor their exit order change that choice. Docker restarts manually started containers after reboot; automatic sessions and explicitly stopped sessions stay stopped. Harness processes and terminal attachments are not resumed.

Mount, environment, port, and raw Docker validation rules are in [configuration](configuration.md#container-settings).

## Inspection

| Command | Output |
|---|---|
| `list [folder] [--sort folder\|name\|last-active] [--wide] [--json]` | Saved sessions with local names, including missing containers; global view includes folder paths; `--wide` adds full container names |
| `status <folder\|session> [--json]` | Saved details, live commands, container state, and pending configuration changes |
| `status [--json]` | Container state and configuration health for all saved environments |

Bare `status` checks all environments, like bare `list`; `--name` requires a folder target. Checks compare local inputs, not upstream releases. Invalid desired configuration does not hide saved session details. Unmatched managed containers are reported separately. Single-target status says when managed-file changes apply on container restart and suggests `recreate` for image/container changes.

See [output and errors](output.md) for columns, change classifications, and JSON fields.

## Configuration commands

| Command | Effect |
|---|---|
| `config create [name\|path]` | Open the creation overview; supplied names/paths prefill it; reject an existing `config.json` |
| `config edit <name\|path>` | Edit an existing directory or add missing optional files |
| `config list [--json]` | Show named configs under the selected home's `configs/` with harness and directory path; report invalid or incomplete entries; no Docker required |
| `config delete <name> [--force] [--json]` | Delete an unreferenced named config and its files; confirm in a terminal, or use `--force` to skip confirmation (required with `--json`) |
| `edit <folder>` | Pick a session to edit its selected configs or make it the folder default; clear a saved default from the folder menu |
| `edit <folder> --name NAME` | Edit that session's selected configs directly |
| `edit <full-name>` | Edit an exact session's selected configs directly |
| `edit <folder\|session> --show [--json]` | Inspect combined configuration; folder targets require `--name` |

Creation and session-edit menus offer **Add existing config** and **Create config**. Creation uses the same setup workflow as `config create`, then adds the created config to the selected chain. An empty picker also offers **Create and add config**. Cancelling setup leaves the parent choices intact; a successfully created config remains saved even if session creation is cancelled. **Reorder configs** appears only with two or more configs. The picker shows named configs as fixed references in a table; entered paths are saved as relative or fixed according to [reference rules](configuration.md#locations-and-references). Completed settings and config-selection edits save immediately; **Exit** does not roll them back. After changing a session's selected configs, the exit receipt gives an exact-session `status` command; after config directory edits, it gives bare `status` to review pending changes across environments. See [editing controls](configuration.md#editing-and-inspection).

Both directory commands accept setup flags:

| Flag | Meaning |
|---|---|
| `--harness NAME` | Set the persistent Harness setting |
| `--artifact NAME` | Add missing `harness-config`, `setup.sh`, `before-open.sh`, or `Dockerfile`; repeatable or comma-separated |
| `--artifact-harness NAME` | Select the file-generation target without changing Harness; requires `harness-config` |
| `--json` | Print the operation result and never prompt |

Explicit setup flags run directly, even in a terminal. Without flags, interactive create/edit open their own menus. A name/path is required for non-interactive or explicit-flag creation. Without setup flags, non-interactive creation writes the minimal config; non-interactive editing requires an explicit operation. Harness-file generation uses `--artifact-harness`, otherwise the config's own selection, and fails if neither supplies a target. Other artifacts need no harness. Existing files are preserved.

## Folder defaults

| Command | Effect |
|---|---|
| `edit <folder>` | Pick a session to edit its selected configs, or choose **Set folder default** or **Clear folder default** from the folder menu |
| `edit <folder> --name NAME --default` | Select directly without a terminal |
| `edit <full-name> --default` | Select that session for its recorded workspace |
| `edit <folder\|session> --clear-default` | Clear the workspace's default without selecting another session |

**Set folder default** opens a picker with the same folder header, row positions, statuses, and default marker as the overview. Choose a session to save, or **Back** to leave the default unchanged. Set and Clear remain folder-level actions.

`--default` requires `--name` or an exact full session name. `--clear-default` cannot be combined with `--name`, `--default`, or `--show`. In the folder overview, **Exit** leaves completed config-selection/default changes saved; selecting a broken session record shows its error and lets you choose again. Selection never starts a container. Opening does not change the default.

## SSH sharing

```sh
devbox-neo ssh <folder|session> <destination> [--host-master]
```

| Item | Contract |
|---|---|
| Terminal | Interactive host terminal; connection remains in the foreground |
| Default mode | SSH master runs inside the container |
| `--host-master` | Dedicated host master; warns before authentication; not remembered |
| Destination | Hostname, SSH alias, IP address, or `user@host` |
| Client config | `/devbox/ssh/config`; exact command printed after authentication |
| Alias | Simple aliases retain their name; username/IPv6 destinations use `ssh-<hash>` |
| Concurrent use | Multiple destinations allowed; active duplicate refused |
| End connection | Ctrl-C revokes the master and its sessions; successful cleanup prints `Disconnected.` |
| Failure | No automatic reconnect or fresh-login fallback through generated clients |

Normal OpenSSH configuration where the master runs supplies keys, ports, ProxyJump, agent forwarding, and X11 forwarding. Devbox does not copy host configuration or credentials. For a reused session, client `-A` or `-X`/`-Y` requests forwarding; the master must also permit it and have an agent/display available. Generated client config does not copy forwarding preferences.

SSH sharing participates in active-command protection and automatic shutdown unless the session was manually started. Its socket mount must be present in the recorded container layout; otherwise recreate the environment. Connections are excluded from transfers. See the [SSH guide](../guides/ssh.md) for the workflow and host-mode security implications.

## Networks

| Command | Effect |
|---|---|
| `network inspect <folder\|session>` | Inspected networks, addresses, and gateways as JSON |
| `network env <folder\|session> [--get NAME]` | Shell-safe exports, or one raw variable value |
| `network connect <network> <folder\|session>` | Attach an existing secondary network; already attached is a no-op |
| `network disconnect <network> <folder\|session>` | Detach a secondary network; primary network cannot be removed |

Attachments do not edit configuration and survive stop/start, not recreation. Host-network containers reject secondary attachments. Devbox does not create or delete user networks.

Network exports include `DEVBOX_HOST`, `DEVBOX_NETWORK`, `DEVBOX_PRIMARY_NETWORK`, and `DEVBOX_DEFAULT_GATEWAY_IP`. In-container copies are `/devbox/network/env` and `/devbox/network/inspect.json`. They refresh during preparation/access and managed network changes; external Docker changes appear at the next refresh.

## Transfers

| Command | Effect |
|---|---|
| `copy <folder\|session> <destination-folder> [--as NAME]` | Copy state with a new session ID; source must be stopped/absent; destination stays stopped |
| `copy <folder\|session> <destination-folder> --move [--as NAME]` | Preserve ID and running intent, then remove source |
| `copy <folder\|session> --as NAME [--move]` | Copy or move to another name in the source workspace |

| Option | Meaning |
|---|---|
| `--name NAME` | Select a source local name within a folder target |
| `--as NAME` | Choose the destination local name; otherwise preserve the source name |
| `--move` | Remove the source after the destination is ready |
| `--dry-run` | Preview without copying state |
| `--json` | Print the result as JSON |

An omitted destination folder means the source's recorded workspace, not the invoking directory. Source and destination identities must differ; collisions fail without replacement. Required destination config directories must already be available.

References keep their form and order: workspace-relative references rebase to the destination, while fixed references keep their absolute paths. Config directories are not copied. Copy leaves defaults untouched; move clears a matching source default without selecting the destination.

JSON transfer results and pending summaries report `mode: "clone"` for `copy` and `mode: "relocate"` for `copy --move`.

Both environments must have no active Devbox commands, the destination must be unused, and the harness must support copying or moving its state. Destination configuration controls creation. Saved harness state and managed-file tracking are copied; workspace files, container-local tools, auth, shared caches, and live connections are not.

An unfinished transfer blocks changes to both environments, including forced deletion. Fix the reported problem and retry the same command. If preparation failed, keep the destination configuration unchanged until the retry succeeds. Cleanup retries do not repeat a completed copy.

## Deletion

```sh
devbox-neo delete <folder|session>... [--container|--session]
devbox-neo delete [filters...] --container|--session
```

Without a scope flag, interactive deletion identifies a folder target's saved default, then asks whether to remove selected containers immediately and separately whether to remove saved data/history. Both prompts default to no. Answering no to the second question retains saved data but does not restore containers removed at the first step. Explicit scope is required for scripts, JSON output, and dry runs, and skips prompts.

| Flag | Meaning |
|---|---|
| `--container` | Remove containers; retain session records, stores, and image tags |
| `--session` | Remove whole environments, including saved history and verified session image tags |
| `--force` | Permit interrupting attached container commands; saved-state deletion still requires idle sessions |
| `--dry-run` | Preview without deletion |
| `--json` | Structured result; see [output](output.md#deletion-results) |
| `--all` | Select all saved environments and unmatched managed containers |
| `--stopped` | Select existing stopped containers; exclude missing containers |
| `--orphaned` | Select saved environments without containers |
| `--older-than DURATION` | Select last Devbox activity older than a positive duration, e.g. `720h` |

`--container` and `--session` are mutually exclusive. Workspace files, configuration, managed auth, and shared caches are outside both scopes.

Exact targets cannot be combined with selection filters. Without targets, provide at least one selection filter. `--name` requires an explicit folder target and cannot be combined with bulk filters. Filters intersect, so `--stopped --orphaned` selects nothing. Unknown activity prevents age-filtered deletion rather than counting as old.

Devbox preflights the complete selection and rechecks activity, container absence, and active commands under lock. Container removal must succeed before saved data can be deleted. Failure or cancellation retains remaining saved state but does not restore containers already removed.

## Completion

`completion bash|zsh|fish|powershell` prints a shell script. For Bash:

```sh
source <(devbox-neo completion bash)
```

Scripts complete commands, flags, config names/paths, harnesses, full session targets, folder-scoped local names, and fixed values. They also register an existing `dbx` shortcut without defining or changing it. For command-name-based Bash/Fish autoloading, install the script under the shortcut's completion filename too, or source it at startup. Zsh's autoload header covers both names.

Suggestions honor the selected home. Completion is read-only and tolerates unavailable Docker or state sources. Reload generated scripts after updating the CLI.

## Version

`version` prints the build version.
