# Commands

Executable: `devbox-neo`. Use `<command> --help` for command-specific help.

## Global options and targets

| Option | Meaning |
|---|---|
| `--home PATH` | Select the Devbox home; overrides `DEVBOX_HOME`, then `~/.devbox-neo` |
| `--profile NAME`, `-p NAME` | Select a named profile environment; excludes project configuration |

A `<target>` is a workspace folder or an exact environment name from `list`. Exact names retain their recorded slot even when defaults change. Folder-based `open` uses normal configuration selection; other lookup commands require an unambiguous saved environment unless a profile is supplied.

## Environment lifecycle

| Command | Effect |
|---|---|
| `create <folder>` | Build and prepare a new environment; leave it stopped; refuse an existing session |
| `open <target> [-- harness-args...]` | Launch the recorded harness in an existing environment |
| `start <target>` | Start without launching the harness |
| `stop <target> [--force]` | Stop; `--force` permits interrupting attached commands |
| `shell <target>` | Open the configured shell in `/workspace` |
| `exec <target> -- <argv...>` | Run exact arguments, without implicit shell interpretation |
| `logs <target> [-f] [--tail N\|all]` | Read Docker logs; default tail `100`; `-f`/`--follow` streams |
| `recreate <target> [--image]` | Replace the container using current configuration; preserve session identity and stores |
| `recreate --all [--image]` | Preflight and recreate all selected containers; optional profile filter |

`open` and `start` can restore a missing container from retained session state when recorded inputs remain available. `shell`, `exec`, and `ssh` require the container to exist. None of these commands creates a new session.

Before a stopped container starts, access commands resolve participating configuration and synchronize compatible managed files. Invalid config or conflicting shared JSON blocks startup. Running access does not synchronize files; running `start`, `shell`, `exec`, and `ssh` also skip desired-config resolution. `open` still resolves launch settings and reports pending creation changes.

`recreate` reuses an available recorded image if image inputs are unchanged; otherwise it builds with cache. `--image` forces a no-cache build. Recreation preserves running/stopped intent but loses container-local changes. Docker logs do not include a transcript of attached `exec` output.

### Creation and launch options

| Option | Commands | Meaning |
|---|---|---|
| `--harness NAME` | create, recreate | Harness to install |
| `--env KEY=VALUE`, `-e` | create, recreate | Environment assignment; repeatable |
| `--volume SOURCE:TARGET[:OPTIONS]`, `-v` | create, recreate | Additional mount; repeatable |
| `--port [HOST_IP:]HOST_PORT:CONTAINER_PORT` | create, recreate | Published port; repeatable |
| `--network NAME` | create, recreate | `default`, `host`, or an existing network |
| `--read-only` | create, recreate | Mount the workspace read-only |
| `--docker-arg=--option=value` | create, recreate | Validated Docker option; repeatable |
| `--on-exit stop\|running` | create, recreate, open | Policy after the last attached command exits |
| `--harness-arg ARG` | create, recreate, open | Appended harness argument; repeatable |
| `--continue`, `-c` | open | Append the harness's continuation arguments |
| `-- <args...>` | open | One-off harness arguments, appended last |

Mount, environment, port, and raw Docker validation rules are in [configuration](configuration.md#creation-options).

## Inspection

| Command | Output |
|---|---|
| `list [--sort name\|last-active] [--wide] [--json]` | Saved environments, including those without containers; default sort `name` |
| `status <target> [--json]` | Saved details, live commands, container state, and pending configuration changes |
| `status --all [--json]` | Container state and configuration health for all saved environments |

`--profile` filters list and bulk status. `status --all` cannot be combined with a target. Checks compare local inputs, not upstream releases. Invalid desired configuration does not hide saved session details. Unmatched managed containers are reported separately.

See [output and errors](output.md) for columns, change classifications, and JSON fields.

## Configuration-owner commands

These commands edit configuration, not containers.

| Command | Effect |
|---|---|
| `profile create <name> [--json]` | Create sparse profile configuration |
| `project create <folder> [--json]` | Create sparse `.devbox/config.json`; folder must exist |
| `project create <folder> --from-profile NAME [--json]` | Copy profile source artifacts once; set `inherit_profile: false`; require an unused destination |
| `profile init <name> [--harness NAME] [--artifact NAME,...] [--json]` | Select a harness and seed missing artifacts |
| `project init <folder> [--harness NAME\|inherit] [--artifact NAME,...] [--json]` | Initialize project artifacts; `inherit` validates the profile/global harness selection |
| `profile list [--json]` | Sorted profiles, default marker, and invalid-config diagnostics |
| `profile set [name]` | Set the default profile; omitted name prompts |
| `profile set --clear` | Clear the default profile |
| `profile delete <name> [--force] [--json]` | Delete profile files only; `--force` skips confirmation |
| `global config` | Edit global settings |
| `profile config <name>` | Edit profile settings |
| `project config <folder>` | Edit project settings |

`create` selects no harness or default profile. `init` preserves existing files; artifacts are `harness-config`, `setup.sh`, `entrypoint.sh`, and `Dockerfile`. Interactive init offers missing choices. Non-interactive init needs an existing harness selection or `--harness`; `--json` never prompts. Profile deletion retains global defaults and existing environments.

Config commands accept `--show [--json]` for effective values without a menu. `project config --show` also accepts `--profile NAME`. Editing requires a terminal; each valid operation saves immediately. See [configuration editing](configuration.md#editing-and-inspection).

## SSH sharing

```sh
devbox-neo ssh <target> <destination> [--host-master]
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

SSH sharing participates in active-command protection and `on_exit`. Its socket mount must be present in the recorded container layout; otherwise recreate the environment. Connections are excluded from transfers. See the [SSH guide](../guides/ssh.md) for the workflow and host-mode security implications.

## Networks

| Command | Effect |
|---|---|
| `network inspect <target>` | Inspected networks, addresses, and gateways as JSON |
| `network env <target> [--get NAME]` | Shell-safe exports, or one raw variable value |
| `network connect <network> <target>` | Attach an existing secondary network; already attached is a no-op |
| `network disconnect <network> <target>` | Detach a secondary network; primary network cannot be removed |

Attachments do not edit configuration and survive stop/start, not recreation. Host-network containers reject secondary attachments. Devbox does not create or delete user networks.

Network exports include `DEVBOX_HOST`, `DEVBOX_NETWORK`, `DEVBOX_PRIMARY_NETWORK`, and `DEVBOX_DEFAULT_GATEWAY_IP`. In-container copies are `/devbox/network/env` and `/devbox/network/inspect.json`. They refresh during preparation/access and managed network changes; external Docker changes appear at the next refresh.

## Transfers

| Command | Effect |
|---|---|
| `clone <source> <destination-folder> [--profile NAME]` | Copy saved harness state with a new session ID; source must be stopped/absent; destination stays stopped |
| `relocate <source> <destination-folder>` | Move saved state, preserve ID and running/stopped intent, then remove source |
| `clone\|relocate <folder> --from SLOT --to SLOT` | Transfer between same-folder slots: profile names or `.project` |

Both commands accept `--dry-run` and `--json`. Use an exact source name if a folder is ambiguous. Cross-folder transfers retain the source slot; clone's `--profile` selects the destination profile, not the source. Same-folder transfers require both `--from` and `--to`, without a destination folder or `--profile`. Relocate changes slots only through `--from`/`--to`. Destination profiles must exist; project destinations must be initialized.

Transfers require idle endpoints, an unused destination, and harness portability declarations. Destination configuration controls creation. Only declared environment stores and managed-config manifests are copied—not workspace files, container-layer tools, auth, shared caches, active commands, or SSH connections.

Pending transfers reserve both endpoints and block ordinary mutations, including forced deletion. Retry the same command to resume. Before destination commitment, destination inputs must match the journal. After commitment, retry finishes recorded recovery and cleanup without copying again.

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
