# Commands

Prefix commands below with `devbox-neo`. Use `<command> --help` for options. For a walkthrough, start with [Getting started](../guides/getting-started.md).

## Global options and targets

| Input | Meaning |
|---|---|
| `--home PATH` | Devbox home; overrides `DEVBOX_HOME`, then `~/.devbox-neo` |
| `<target>` | A project folder or session ID from `list --wide` |
| `--name NAME` | Select a folder-local session instead of the folder default |
| `.` | Current folder |

A folder alone requires a saved default, even if it has only one session. An exact session ID works from any directory. A local name alone is not a target.

## Interactive frontend

| Command | Opens |
|---|---|
| No subcommand | Session browser |
| `config` | Local and named-config browser |

Enter opens the selected object's menu; it does not launch a harness. Sessions expose lifecycle, config, network, transfer, rename, SSH, and deletion operations directly. See [keyboard controls](output.md#interactive-menus).

These browsers require terminal input/output. Otherwise, including `TERM=dumb`, they show help. Explicit commands remain available for scripts.

## Environment lifecycle

| Command | Effect |
|---|---|
| `create <folder> --name NAME --config REF` | Create a stopped session; repeat `--config` for an ordered list |
| `open <target> [-- args...]` | Launch the harness |
| `start <target>` | Keep running until Stop, including after Docker restarts |
| `stop <target> [--force]` | Stop and clear keep-running intent |
| `shell <target>` | Open a shell in `/workspace` |
| `exec <target> -- <argv...>` | Run a command without implicit shell parsing |
| `logs <target> [-f] [--tail N\|all]` | Docker logs; default tail `100` |
| `recreate <target> [--image]` | Apply current config by replacing the container |
| `recreate --all [--image]` | Recreate all managed containers |

Creation never selects a default or launches the harness. In a terminal, missing name/config inputs open the creation form; scripts must supply both. Container settings belong in configs, not `create` flags.

`open` and `start` can restore missing containers when recorded inputs remain available. Shell, Exec, and SSH require an existing container.

Recreation preserves saved harness state and running intent, but **loses container-local files and tools**. `--image` disables build cache. Docker logs are not harness conversation transcripts.

### Creation and launch options

| Option | Applies to | Meaning |
|---|---|---|
| `--name NAME` | create | Required local name; prompted when omitted in a terminal |
| `--config REF` | create | Config name/path; repeat in application order |
| `--continue`, `-c` | open | Resume the previous harness conversation |
| `--harness-arg ARG` | open | One-off argument; repeatable |
| `-- args...` | open | One-off arguments appended last |

Configured arguments precede continuation and one-off arguments. One-off arguments are not saved. Without manual Start, the last attached command stops the container.

## Inspection

| Command | Output |
|---|---|
| `list [folder] [--sort folder\|name\|last-active] [--wide] [--json]` | Sessions, including missing containers; `--wide` adds session IDs and container names |
| `status [--json]` | Configuration health for all sessions |
| `status <target> [--json]` | One session's state, active commands, and pending changes |

Checks compare local inputs, not upstream releases. See [output formats](output.md).

## Configuration commands

| Command | Effect |
|---|---|
| `config create [name\|path]` | Create a config through the overview; supplied destination prefills it |
| `config edit <name\|path>` | Edit settings or add optional files |
| `config list [--json]` | Named configs under the selected home, including invalid entries |
| `config show <name\|path> [--json]` | One config over built-in defaults, with provenance and redaction |
| `config users <name\|path> [--json]` | Saved sessions using the config directory |
| `config delete <name> [--force] [--json]` | Delete an unused named config and its files |
| `edit <folder>` | Choose a session's configs or change the folder default |
| `edit <folder> --name NAME` | Edit a folder-local session's selected configs |
| `edit <session-id>` | Edit an exact session's selected configs |
| `edit <target> --workspace PATH [--json]` | Save a new workspace reference; requires explicit recreation and clears a matching old-folder default |
| `edit <target> --show [--json]` | Combined settings; folder targets require `--name` |
| `edit <target> --config REF [--config REF…] [--json]` | Replace the entire ordered config selection; folder targets require `--name` |

`--config` requires at least one reference and replaces the list in flag order. Config, workspace, inspection, and default operations cannot be combined. `--workspace` requires a session ID or explicit `--name`.

Settings edits save immediately. Nested config creation adds its result to the session draft; that config remains saved if the draft is cancelled. [Config reference](configuration.md) covers paths and merge rules.

`config create` rejects an existing `config.json`. Optional-file creation preserves existing files. `config delete` accepts direct named directories only, not symlinks or arbitrary paths; it refuses configs still needed by saved sessions. Its `--force` skips confirmation, not usage checks, and is required with `--json`.

Create/edit support these setup flags:

| Flag | Meaning |
|---|---|
| `--harness NAME` | Set the config's harness |
| `--artifact NAME` | Add `harness-config`, `setup.sh`, `before-open.sh`, or `docker/Dockerfile`; repeatable/comma-separated |
| `--artifact-harness NAME` | Harness whose files to generate; requires `harness-config` |
| `--json` | Structured result; never prompt |

Supplying setup flags runs directly. A destination is required outside interactive creation. Harness-file generation uses `--artifact-harness`, then the config's harness; it fails if neither selects one. Non-interactive edit requires an explicit operation.

## Folder defaults

| Command | Effect |
|---|---|
| `edit <folder>` | Set/Clear default from the folder overview |
| `edit <folder> --name NAME --default` | Select a named session |
| `edit <session-id> --default` | Select that exact session |
| `edit <target> --clear-default` | Clear without selecting a replacement |

The browser offers Make/Clear folder default in the session menu. Selection does not launch anything. `--clear-default` cannot combine with `--name`, `--default`, `--show`, or `--config`.

## SSH sharing

```sh
devbox-neo ssh <target> <destination> [--host-master]
```

| Option / input | Meaning |
|---|---|
| Destination | Hostname, SSH alias, IP address, or `user@host` |
| Default mode | SSH runs inside the container |
| `--host-master` | Use the host's SSH configuration and network; invocation-only |
| Client config | `/devbox/ssh/config`; exact client command printed after authentication |
| End sharing | Ctrl-C in the host terminal |

Requires a foreground host terminal for authentication. Multiple destinations may be shared; active duplicates are refused. Normal OpenSSH settings control keys, ports, ProxyJump, agent forwarding, and X11 forwarding. Client forwarding requests require support from the master.

**Host mode grants the container broader host/network access.** Connections are not transferred or automatically reconnected. See the [SSH guide](../guides/ssh.md).

## Networks

| Command | Effect |
|---|---|
| `network inspect <target>` | Networks, addresses, and gateways as JSON |
| `network env <target> [--get NAME]` | Shell exports, or one raw value |
| `network connect <network> <target>` | Attach an existing secondary network |
| `network disconnect <network> <target>` | Detach a secondary network |

Attachments survive stop/start, not recreation. The primary network cannot be detached. Host networking rejects port publishing and secondary attachments. Devbox does not create user networks.

Exports include `DEVBOX_HOST`, `DEVBOX_NETWORK`, `DEVBOX_PRIMARY_NETWORK`, and `DEVBOX_DEFAULT_GATEWAY_IP`. Container copies live under `/devbox/network/` and refresh during Devbox access/preparation and network changes.

## Transfers

```sh
devbox-neo copy <target> [destination-folder] [--as NAME] [--move]
devbox-neo rename <target> --to NAME [--dry-run] [--json]
```

| Option | Meaning |
|---|---|
| `--name NAME` | Source session within a folder target |
| `--as NAME` | Destination local name; otherwise retain the source name |
| `--move` | Remove the source after the destination is ready |
| `--dry-run` | Preview without transferring |
| `--json` | Structured result |

Omitting the destination keeps the source workspace. The destination must be unused, both endpoints idle, and destination configs available. Copy requires a stopped/absent source and leaves the destination stopped; Move preserves running intent.

Only declared harness state transfers. Project/config files, container-local tools, auth, caches, and live connections do not. Relative config references follow the destination; fixed ones keep their paths. Neither operation selects a destination default.

For interrupted transfers, fix the reported problem and retry the same command. Keep pending state and destination config in place until recovery finishes. JSON uses `clone` for Copy and `relocate` for Move.

`rename --to NAME` changes only the session label; storage, container, history, and default are unchanged. It accepts `--name` for source selection, `--dry-run`, and `--json`, and runs without prompting.

## Deletion

```sh
devbox-neo delete <target>... [--container|--session]
devbox-neo delete [filters...] --container|--session
```

| Flag | Meaning |
|---|---|
| `--container` | Remove containers; retain saved session data and images |
| `--session` | Remove containers, saved data/history, and session image tags |
| `--force` | Allow interruption of attached container commands |
| `--dry-run` | Preview |
| `--json` | Structured result |
| `--all` | All saved sessions and unmatched managed containers |
| `--stopped` | Existing stopped containers |
| `--orphaned` | Saved sessions without containers |
| `--older-than DURATION` | Recorded inactivity longer than a positive duration, such as `720h` |

**Scope flags skip confirmation** and are required for scripts, JSON, and dry runs. Without one, a terminal asks about containers first, then saved data. Declining the second question does not undo container removal.

The browser instead displays the chosen scope and confirms its applicable phases. Container only never removes saved data.

Scopes are mutually exclusive. Exact targets cannot combine with filters; filters intersect. `--name` requires an explicit target. Force does not expand scope or bypass saved-data idle checks. Project files, configs, auth, and caches are outside both deletion scopes.

## Completion

For persistent Bash setup, add these lines to `~/.bashrc`, replacing the checkout path:

```bash
export PATH="/path/to/devbox/bin:$PATH"
source <(devbox-neo completion bash)
```

Reload with `source ~/.bashrc`. Other shells use `completion zsh`, `completion fish`, or `completion powershell`.

An optional `alias dbx='devbox-neo'` is supported by generated completions; Devbox does not create the alias. Completion is read-only. Regenerate/reload scripts after updating the CLI.

## Version

`version` prints the build version.
