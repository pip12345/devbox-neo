# Commands

Executable: `devbox-neo`. Use `<command> --help` for command-specific help.

## Global options and targets

| Option | Meaning |
|---|---|
| `--home PATH` | Select the Devbox home; overrides `DEVBOX_HOME`, then `~/.devbox-neo` |
| `--profile NAME`, `-p NAME` | Select the base profile; retain participating project configuration |
| `--ignore-project` | Exclude project configuration and artifacts; on bulk commands, exclude project-participating sessions |

`<folder|session>` accepts a workspace folder or an exact saved session name from `list`. Every folder-targeted command selects the current profile/project combination and fails if that session does not exist; it never substitutes another profile. Exact names retain their recorded combination even when defaults change, and conflicting explicit selection flags fail. The session and its replaceable container share a name; there is no separate container selector.

Profile/project configuration commands work without an existing environment. `project config` also accepts a session name to select its workspace.

## Environment lifecycle

| Command | Effect |
|---|---|
| `create <folder>` | Build and prepare a new environment; leave it stopped; refuse an existing session |
| `open <folder\|session> [-- harness-args...]` | Launch the recorded harness in an existing environment |
| `start <folder\|session>` | Keep running until explicit `stop`, including automatic restart when Docker starts after reboot |
| `stop <folder\|session> [--force]` | Stop and clear manual keep-running intent; `--force` permits interrupting attached commands |
| `shell <folder\|session>` | Open the configured shell in `/workspace` |
| `exec <folder\|session> -- <argv...>` | Run exact arguments, without implicit shell interpretation |
| `logs <folder\|session> [-f] [--tail N\|all]` | Read Docker logs; default tail `100`; `-f`/`--follow` streams |
| `recreate <folder\|session> [--image]` | Replace the container using current configuration; preserve session identity and stores |
| `recreate --all [--image]` | Preflight and recreate all selected containers; optional profile filter |

`open` and `start` can restore a missing container from retained session state when recorded inputs remain available. `shell`, `exec`, and `ssh` require the container to exist. None of these commands creates a new session.

Before a stopped container starts, access commands resolve participating configuration and synchronize compatible managed files. Invalid config or conflicting shared JSON blocks startup. Running access does not synchronize files; running `start`, `shell`, `exec`, and `ssh` also skip desired-config resolution. `open` still resolves launch settings and reports pending creation changes.

`recreate` reuses an available recorded image if image inputs are unchanged; otherwise it builds with cache. `--image` forces a no-cache build. Recreation preserves running/stopped intent but loses container-local changes. Docker logs do not include a transcript of attached `exec` output.

### Creation and launch options

Configure container settings through `profile config <profile>` or `project config <folder>` before creation/recreation. Selection flags identify the session; `recreate --image` controls rebuilding.

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
| `list [--sort name\|last-active] [--wide] [--json]` | Saved environments, including those without containers; default sort `name` |
| `status <folder\|session> [--json]` | Saved details, live commands, container state, and pending configuration changes |
| `status --all [--json]` | Container state and configuration health for all saved environments |

`--profile` filters list and bulk status. `status --all` cannot be combined with a target. Checks compare local inputs, not upstream releases. Invalid desired configuration does not hide saved session details. Unmatched managed containers are reported separately.

See [output and errors](output.md) for columns, change classifications, and JSON fields.

## Configuration commands

These commands edit configuration, not containers.

| Command | Effect |
|---|---|
| `profile create <name> [--json]` | Create sparse profile configuration |
| `project create <folder> [--json]` | Create sparse `.devbox/config.json`; folder must exist |
| `project create <folder> --from-profile NAME [--json]` | Copy profile source artifacts once; set `inherit: false`; require an unused destination |
| `profile init <name> [--harness NAME] [--artifact NAME,...] [--json]` | Select a harness and seed missing artifacts |
| `project init <folder> [--harness NAME\|inherit] [--artifact NAME,...] [--json]` | Initialize project artifacts; `inherit` validates the profile/global harness selection |
| `profile list [--json]` | Sorted profiles, default marker, and invalid-config diagnostics |
| `profile set [name]` | Set the default profile; omitted name prompts |
| `profile set --clear` | Clear the default profile |
| `profile delete <name> [--force] [--json]` | Delete profile files only; `--force` skips confirmation |
| `global config` | Edit global settings |
| `profile config <profile>` | Edit profile settings |
| `project config <folder\|session>` | Edit the workspace's `.devbox/` configuration |

`profile create` and `project create` create minimal configuration without choosing a harness or default profile. `profile delete` removes its files but leaves the default-profile setting and existing environments unchanged.

`init` preserves existing files. Available artifacts are `harness-config`, `setup.sh`, `before-open.sh`, and `Dockerfile`. Interactive init offers missing files to add. Non-interactive init needs an existing harness selection or `--harness`; `--json` never prompts.

Config commands accept `--show [--json]` for effective values without a menu. `project config --show` also accepts `--profile NAME`. Editing requires a terminal; each valid operation saves immediately. See [configuration editing](configuration.md#editing-and-inspection).

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
| `copy <folder\|session> <destination-folder> [--to SLOT]` | Copy saved harness state with a new session ID; source must be stopped/absent; destination stays stopped |
| `copy <folder\|session> <destination-folder> --move` | Move saved state, preserve ID and running/stopped intent, then remove source |
| `copy <folder> --from SLOT --to SLOT [--move]` | Transfer between same-folder slots: `.profile-NAME`, `.profile-NAME.project`, or `.project` |

| Option | Meaning |
|---|---|
| `--profile NAME` | Select the source's base profile |
| `--from SLOT` | Select the source by its exact name suffix, such as `.profile-basic`; conflicts with `--profile` |
| `--to SLOT` | Select destination configuration: `.profile-NAME`, `.profile-NAME.project`, or `.project`; destination `inherit` rules apply |
| `--move` | Remove the source after the destination is ready |
| `--dry-run` | Preview without copying state |
| `--json` | Print the result as JSON |

Without a destination folder, `--to` is required. Cross-folder copies keep the source's profile/project combination unless `--to` selects another; cross-folder moves must keep that combination.

Any selected destination profile must exist. If project configuration is selected, initialize the destination's `.devbox/` first. Transfers do not copy project configuration.

JSON transfer results and pending summaries report `mode: "clone"` for `copy` and `mode: "relocate"` for `copy --move`.

Both environments must have no active Devbox commands, the destination must be unused, and the harness must support copying or moving its state. Destination configuration controls creation. Saved harness state and managed-file tracking are copied; workspace files, container-local tools, auth, shared caches, and live connections are not.

An unfinished transfer blocks changes to both environments, including forced deletion. Fix the reported problem and retry the same command. If preparation failed, keep the destination configuration unchanged until the retry succeeds. Cleanup retries do not repeat a completed copy.

## Deletion

```sh
devbox-neo delete <target...> [--container|--session]
devbox-neo delete [filters...] --container|--session
```

Without a scope flag, interactive deletion asks about containers first, then saved data/history. Both prompts default to no. Explicit scope is required for scripts, JSON output, and dry runs, and skips prompts.

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

Exact targets cannot be combined with selection filters. Without targets, provide at least one selector; `--profile` alone is not enough. Filters intersect, so `--stopped --orphaned` selects nothing. Unknown activity prevents age-filtered deletion rather than counting as old.

Devbox preflights the complete selection and rechecks activity, container absence, and active commands under lock. Container removal must succeed before saved data can be deleted. Failure or cancellation retains remaining saved state but does not restore containers already removed.

## Completion

`completion bash|zsh|fish|powershell` prints a shell script. For Bash:

```sh
source <(devbox-neo completion bash)
```

Scripts complete commands, flags, profiles, harnesses, environment targets, and fixed values. They also register an existing `dbx` shortcut without defining or changing it. For command-name-based Bash/Fish autoloading, install the script under the shortcut's completion filename too, or source it at startup. Zsh's autoload header covers both names.

Suggestions honor the selected home. Completion is read-only and tolerates unavailable Docker or state sources. Reload generated scripts after updating the CLI.

## Version

`version` prints the build version.
