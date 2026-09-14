# Commands — current development checkpoint

Executable: `devbox-neo`. `--home` selects an isolated home; the default is `~/.devbox-neo`. `--profile` selects an explicit profile slot. See [progress](../../dev/progress.md) for unfinished capabilities.

## Environments

A session is the saved environment; its container is disposable runtime. Environment commands are top-level. There is no `session` command group or separate container-only list/status command.

| Command | Behavior |
|---|---|
| `create <folder>` | Create a new environment, run preparation/setup, and leave it stopped without launching its harness; refuse an existing session |
| `open <folder-or-name> [-- harness-args...]` | Resolve desired configuration and open an existing session; valid creation drift warns first without replacing; never create a new session |
| `list [--sort name\|last-active] [--wide] [--json]` | Saved environments with harness, profile, activity, container state, and folder; default sort is name |
| `status <target> [--json]` | Session ID, harness, image, active-command count, and live state plus desired drift or a separate configuration error |
| `status --all [--profile NAME] [--json]` | All saved environments, including missing containers, with separate container state and configuration health |
| `start <target>` | Synchronize runtime config and start an existing session; recover a missing container when recorded inputs remain available; never create a new session |
| `shell <target>` | Open the environment's shell; synchronize runtime config first if the container must start |
| `exec <target> -- <argv...>` | Execute exact argv in an existing container |
| `ssh <target> <destination> [--host-master]` | Foreground SSH sharing; user authenticates in a host terminal, container processes reuse `/devbox/ssh/config` |
| `logs <target> [--follow] [--tail N\|all]` | Docker container logs, not a transcript of attached `exec` output |
| `recreate <target> [--image]` | Apply current creation settings while preserving session state |
| `recreate --all [--image]` | Preflight all selected owned containers, then recreate while preserving running/stopped intent |
| `delete <target...> [--container\|--session] [--force]` | Interactive choices without a scope flag; explicit scope deletes without prompts |
| `delete [--all] [--stopped] [--orphaned] [--older-than DURATION] --container\|--session [--dry-run] [--json]` | Filtered cleanup with complete-set preflight; see deletion below |

`create` and `recreate` accept `--harness`, `--env`, `--volume`, `--port`, `--docker-arg`, `--network`, and `--read-only`. All three of `create`, `recreate`, and `open` accept `--on-exit` and `--harness-arg`. `open` additionally accepts `--continue` and arguments after `--`; it has no creation flags. `open` and `start` require an existing session and can still recover its missing container under the recorded recovery conditions. `--profile` retains `-p`; port publishing uses `--port` without that short flag. Config env is recoverable from verified source references; invocation-only env requires explicit recreation after container loss.

`list --sort last-active` orders newest activity first, with name as the tie-breaker and unknown activity last. The default columns are `NAME`, `HARNESS`, `PROFILE`, `LAST ACTIVE`, `CONTAINER`, and `FOLDER`. `--wide` adds the last recorded action and shows exact UTC activity/creation timestamps. Creation time comes from Docker, not session creation. Stopped and missing rows are dimmed on supported terminal output; running rows remain normal. `NO_COLOR`, `TERM=dumb`, and non-terminal output disable styling. `!` marks an error and `*` a pending transfer; their details remain undimmed below the table. `--json` returns an object with `sessions` and `unmatched_containers` arrays, including when empty; session rows follow the selected sorting. Last activity means recorded Devbox operations, not filesystem activity or only harness launches.

`status --all` shows `NAME`, `CONTAINER`, and `CHANGE`. Changes are `No changes`, `Runtime changes`, `Recreate needed`, or `Rebuild + recreate needed`; unresolved configuration, invalid records, ownership/instance mismatches, and pending transfers show `Cannot check` with separate diagnostics. A missing container is a separate fact, not automatically an error. Both recreation cases suggest ordinary `recreate`, which automatically builds changed image inputs. Checks compare current local inputs with recorded fingerprints, not container age or newer upstream releases. `--all` cannot be combined with an exact target. Bulk JSON has `sessions` and `unmatched_containers` arrays; a single target remains one status object. Session rows contain `desired_change`, `pending_input_changes`, `config_error`, `error`, and `pending_transfer` as applicable. Per-row diagnostics do not fail the command; unavailable inventory/Docker does. Ordinary `list` does not resolve desired inputs.

Both `list` and `status --all` warn below their tables about installation-managed containers with no session record. These appear only in `unmatched_containers` in JSON, not as invented session rows. Corrupt records remain session rows with errors. Profile filtering uses recorded session identity, or live slot labels when records are unavailable. Unknown-profile broken records without containers remain visible in unfiltered inventory. Warnings never adopt or delete resources.

`status <target>` includes saved session details even when current configuration is invalid or the container is missing. Its JSON adds `record` (the saved contract) and `active` (live command leases) alongside the status fields; bulk JSON omits these details. An exact pending-transfer endpoint remains inspectable without a session record, in which case `record` is omitted and `active` is empty. Pending transfers skip desired-configuration checks. There is no separate `show` command.

`status <target>` prints detailed reasons; `status --all` groups them below affected rows. Each pending input change has an `image`, `container`, or `runtime` scope. JSON `pending_input_changes` entries contain `scope`, `code`, and `field`, with `key`, `path`, `before`, and `after` where applicable. Codes are `value_changed`, `input_changed`, `entry_added`, `entry_removed`, `order_changed`, `file_added`, `file_removed`, `file_content_changed`, `file_kind_changed`, and `file_mode_changed`. Simple public settings show old/new values; env reasons identify variable names without values or hashes. File contents are never printed. Image reasons distinguish the Dockerfile, ignore rules, included build-context paths, harness definition, generated Devbox image layer, and build arguments. Source-path changes alone do not cause image rebuilding when effective inputs are identical.

When `open` detects container/image drift, it prints those specific creation reasons and the recreation command before other open output or startup work, then continues immediately. Runtime changes are shown by status, not presented as reasons to recreate in this warning.

Exact container names keep their recorded slots when defaults change. Open targets are arguments to `open`, so target names do not collide with top-level commands. `open`, `start`, `shell`, `exec`, and `ssh` synchronize managed runtime config before starting a stopped container. Creation/recreation and transfer destination creation synchronize before startup too. No managed-file synchronization occurs when attaching to a running container, during inspection, stop, deletion, or network commands. Invalid participating configuration or invalid shared JSON blocks startup; errors are not ignored. Running-container start/shell/exec/ssh and logs do not resolve desired configuration. Creation-time settings remain recorded until recreation, and an incompatible harness definition defers its config until recreation. Attached `open`, `shell`, and `exec` forward the invoking terminal's display variables, including `TERM` and `COLORTERM`, without requiring recreation; see [environment precedence](configuration.md#substitution-environment-and-creation-options).

## Errors and next steps

Commands return typed failures with stable codes, safe messages, known targets, and optional `next_steps` argv arrays. Human errors use `Error: <message>` on stderr, with a separate `Target:` line when known and labeled, copyable commands. Codes and operation names stay in JSON rather than the human header. Commands supporting `--json` instead emit one error object on stdout and exit nonzero; success JSON shapes are unchanged. Error fields are `error` (code), `message`, `operation`, optional `target`, `next_steps`, and `related_errors` for joined failures. Interactive `open`, `shell`, `exec`, and `ssh` do not gain a JSON mode, and child output is unchanged. Flag errors do not echo rejected values.

Unknown root/group commands use `unknown_command` with quoted user input and structured suggestion/usage steps, rather than embedding multiline suggestions in an escaped error string.

Common codes include `unknown_command`, `configuration_missing`, `profile_missing`, `harness_required`, `unknown_harness`, `invalid_harness_definition`, `invalid_configuration`, `configuration_unavailable`, `session_missing`, `session_exists`, `create_stop_failed`, `ambiguous_target`, `container_missing`, `session_busy`, `ownership_mismatch`, `container_mismatch`, `managed_config_conflict`, `deletion_scope_required`, `recovery_unavailable`, `pending_transfer`, `transfer_failed`, `docker_unavailable`, and `docker_inventory_unavailable`. Other Docker failures use `docker_command_failed`; unclassified errors use `command_failed`. Cancellation/deadline failures remain nonzero. Docker/child exit status is preserved even when cleanup also fails.

Next steps retain an explicit `--home`, use exact known targets, and never execute automatically. Each command is labeled with its reason; `Then` marks a sequence and `Or` marks an alternative. The recommended action comes first. Generic creation guidance shows only `create <folder>`, without `--profile`. Missing-environment hints preserve the entered target (`.`, a relative path, or an absolute path) and use normal configuration selection; they do not replay the previous invocation's flags. Transfer retries retain the selectors needed for their recorded endpoints. No `--force` action is suggested by default. If a failure requires manual repair, the message identifies what needs attention rather than inventing a repair command. Error reporting does not initialize homes, repair records, or adopt containers.

## Completion

`completion bash|zsh|fish|powershell` prints a shell completion script. Load it in your shell to complete command/flag names, recorded session targets, live container targets, profiles, valid harnesses, transfer slots, and fixed option values. Folder completion remains available where folders are accepted. For Bash, with the executable on `PATH`:

```sh
source <(devbox-neo completion bash)
```

The loaded script registers both `devbox-neo` and `dbx`. It does not define `dbx` or change an existing alias/function; completion invokes that shortcut with its existing flags. Regenerate and reload previously saved scripts to pick up this registration. With command-name-based Bash/Fish autoloading, also install the generated script under the `dbx` completion filename (`dbx` / `dbx.fish`), or source it at shell startup. Zsh's generated autoload header covers both names.

Suggestions honor `--home` / `DEVBOX_HOME`. Completion does not initialize state, create locks, or modify Docker resources. Container suggestions use a bounded, installation-filtered Docker inventory; unavailable Docker quietly leaves folder completion available. Session suggestions use existing session directories, including corrupt records so they can still be addressed. Invalid harness overrides are not suggested and never fall back to the built-in.

## SSH sharing

`ssh` requires an interactive host terminal. Container master is the default; `--host-master` runs a dedicated host master and prints an explicit host/network-access warning before authentication. Configuration, keys, ProxyJump, agent forwarding, and X11 forwarding come from where SSH runs. Devbox copies no credentials or host config. Ports and keys belong in normal SSH configuration; there are no identity, detach, or arbitrary SSH-argument flags.

Destinations are hostnames, SSH aliases, IP addresses, or `user@host`. Simple aliases retain their names; destinations containing a username or IPv6 address use a generated `ssh-<hash>` client alias. Devbox prints the exact client command after authentication. Multiple destinations can coexist; an active duplicate is refused. Missing/dead sockets fail without a new login or direct-connection fallback. Ctrl-C ends the actual master and its active sessions, restores the terminal, and prints `Disconnected.` with a successful exit after cleanup. Genuine SSH failures, deadlines, and cleanup failures remain errors; no automatic reconnect occurs. The command uses normal startup preparation and attached-command lease/on-exit rules. Environments lacking the socket mount need explicit recreation. See [SSH workflow](../guides/ssh.md).

## Networks

| Command | Behavior |
|---|---|
| `network inspect <target>` | Live primary/secondary network facts as JSON |
| `network env <target> [--get NAME]` | Shell-safe exports, or one raw variable value |
| `network connect <network> <target>` | Attach an existing secondary network; already attached is a no-op |
| `network disconnect <network> <target>` | Detach a secondary network, never the configured primary |

Network changes do not edit configuration or fingerprints. They survive stop/start, not recreation. Host-network containers reject secondary attachments. Devbox never creates or deletes user networks. Network exports include `DEVBOX_HOST`, `DEVBOX_NETWORK`, `DEVBOX_PRIMARY_NETWORK`, and `DEVBOX_DEFAULT_GATEWAY_IP`. Inside a running container, `/devbox/network/env` contains shell-safe exports and `/devbox/network/inspect.json` contains the inspected facts. These files refresh during preparation/access and after managed network changes; direct external Docker changes are reflected on the next refresh.

## In-container documentation

`/devbox/AGENTS.md` provides container guidance; `/devbox/docs/index.md` links the embedded human docs. These files are root-owned container runtime data, not session state. Pi/OpenCode defaults include a `devbox` skill pointing to them. Runtime documentation changes do not require an image rebuild.

## Saved state

| Command | Behavior |
|---|---|
| `clone <source> <destination-folder> [--profile NAME] [--dry-run] [--json]` | New session ID; stopped/absent source, stopped destination |
| `relocate <source> <destination-folder> [--dry-run] [--json]` | Preserve ID and running/stopped intent; remove source after destination commitment |
| `clone\|relocate <folder> --from SLOT --to SLOT [--dry-run] [--json]` | Exact same-folder slots; each slot is a profile name or `.project` |

There are no `reset` or `prune` commands. Startup restores managed configuration; filtered cleanup belongs to `delete`. Unmanaged files, history, auth, and caches are not wiped by startup. Transfers require idle endpoints, an unused destination, and portable harness declarations for all copied state. Destination configuration controls creation; no workspace files or container-layer changes are copied. Cross-folder transfers retain the source slot unless clone selects another profile. Project destinations must be initialized. Pending transfers block ordinary mutations, including forced deletion; retry the same command to resume. Before commitment, destination inputs must still match the journal. After commitment, retry uses the recorded destination rather than desired configuration.

## Deletion

Without a scope flag, interactive `delete` asks to remove containers, then separately asks about saved session data/history. Both prompts default to no. Missing-container targets need only the saved-data choice. Container removal must succeed before saved data can be deleted.

| Flag | Meaning |
|---|---|
| `--container` | Delete runtime only, preserving saved records/stores and image tags; no prompts |
| `--session` | Delete the whole environment: container, saved records/stores/history, and verified session image tag; no prompts |
| `--force` | Permit disrupting attached container commands; never expand deletion scope or bypass saved-state idle checks |
| `--dry-run` | Preview the explicit scope without prompting or deleting |
| `--all` | Select all saved environments and unmatched managed containers |
| `--stopped` | Select existing stopped containers; exclude missing containers |
| `--orphaned` | Select saved environments without containers |
| `--older-than DURATION` | Select last recorded activity older than a positive duration, e.g. `720h` for 30 days |

`--container` and `--session` are mutually exclusive. Explicit scope is required for non-interactive/JSON deletion and all dry runs. There is no `--yes` or `--include-session` flag. Auth, shared caches, workspace files, and configuration are never part of either scope.

Exact targets cannot be mixed with selection filters. With no targets, at least one selector is required; `--profile` alone does not imply all. Combined filters intersect; `--all` can be narrowed by other filters. `--stopped --orphaned` therefore selects nothing. Age uses recorded Devbox activity, not creation time or proof of idleness. Unknown/unverifiable activity prevents age-filtered deletion rather than being treated as old. Activity and orphan status are rechecked under the complete lock set before container mutation, including after confirmation; container deletion's own activity update does not invalidate the already-locked selection.

The complete selection is locked and preflighted. Locks stay held across both prompts and deletion phases, so another Devbox command cannot recreate or replace a selected environment between choices. Explicit saved-data deletion is preflighted before container removal and rechecked afterward. If a later step fails or is cancelled, already-deleted containers are not restored; remaining saved state is retained. JSON results contain `containers`, `sessions`, `retained_sessions`, `dry_run`, and `cancelled`.

For numbered configuration menus and scoped config `--show [--json]`, see [configuration](configuration.md#configuration-owner-commands).
