# Commands — current development checkpoint

Executable: `devbox-neo`. `--home` selects an isolated home; the default is `~/.devbox-neo`. `--profile` selects an explicit profile slot. See [progress](../../dev/progress.md) for unfinished capabilities.

## Containers

| Command | Behavior |
|---|---|
| `create <folder>` | Create a new environment, run preparation/setup, and leave it stopped without launching its harness; refuse an existing session |
| `open <folder-or-name> [--create] [-- harness-args...]` | Resolve desired configuration and open an existing session; `--create` opts into creation if missing; valid creation drift warns first without replacing |
| `list [--sort name\|last-active] [--wide] [--json]` | Container table with state, profile, last activity, and folder; default sort is name |
| `status <target> [--json]` | Live state plus desired drift or a separate configuration error |
| `status --all [--profile NAME] [--json]` | Existing managed containers, sorted by name, with separate live state and pending-change classifications |
| `start <target>` | Start an existing session using recorded settings; recover a missing container when recorded inputs remain available; never create a new session |
| `shell <target>` | Use the recorded shell in an existing container |
| `exec <target> -- <argv...>` | Execute exact argv in an existing container |
| `logs <target> [--follow] [--tail N\|all]` | Docker container logs, not a transcript of attached `exec` output |
| `recreate <target> [--image]` | Apply current creation settings while preserving session state |
| `recreate --all [--image]` | Preflight all selected owned containers, then recreate while preserving running/stopped intent |
| `delete <target...>` | Delete containers only; retain session state and image tags |
| `delete --all\|--stopped [--force] [--json]` | Bulk container deletion after complete lock-set preflight; force permits disrupting attached commands |

`create`, `open`, and `recreate` also accept `--harness`, `--harness-arg`, `--env`, `--volume`, `--port`, `--docker-arg`, `--network`, `--on-exit`, and `--read-only`. `--continue` and `--create` belong only to `open`. Plain `open` and `start` require an existing session. They can still recover its missing container under the recorded recovery conditions without `--create`. `open --create` never replaces an existing session and requires a folder for new creation; a missing exact session name is not a creation target. `--profile` retains `-p`; port publishing uses `--port` without that short flag. Config env is recoverable from verified source references; invocation-only env requires explicit recreation after container loss.

`list --sort last-active` orders newest activity first, with name as the tie-breaker and unknown activity last. `--wide` adds the harness and last recorded action, and shows exact UTC activity/creation timestamps. Creation time comes from Docker, not session creation. Stopped and missing rows are dimmed on supported terminal output; running rows remain normal. `NO_COLOR`, `TERM=dumb`, and non-terminal output disable styling. `!` marks an error and `*` a pending transfer; their details remain undimmed below the table. `--json` retains structured records and follows the selected sorting. Last activity means recorded Devbox operations, not filesystem activity or only harness launches.

`status --all` shows `NAME`, `STATE`, and `CHANGE`. Changes are `No changes`, `Runtime changes`, `Recreate needed`, or `Rebuild + recreate needed`; unresolved configuration, invalid/missing records, ownership/instance mismatches, and pending transfers show `Cannot check` with separate diagnostics. Both recreation cases suggest ordinary `recreate`, which automatically builds changed image inputs. Checks compare current local inputs with recorded fingerprints, not container age or newer upstream releases. Retained sessions without containers are excluded; use `session list` to find them. `--all` cannot be combined with an exact target. JSON is an array of the existing status objects, with `desired_change`, `pending_input_changes`, `config_error`, `error`, and `pending_transfer` fields as applicable. Per-row diagnostics do not fail the command; unavailable inventory/Docker does. Ordinary `list` does not resolve desired inputs.

`status <target>` prints detailed reasons; `status --all` groups them below affected rows. Each pending input change has an `image`, `container`, or `runtime` scope. JSON `pending_input_changes` entries contain `scope`, `code`, and `field`, with `key`, `path`, `before`, and `after` where applicable. Codes are `value_changed`, `input_changed`, `entry_added`, `entry_removed`, `order_changed`, `file_added`, `file_removed`, `file_content_changed`, `file_kind_changed`, and `file_mode_changed`. Simple public settings show old/new values; env reasons identify variable names without values or hashes. File contents are never printed. Image reasons distinguish the Dockerfile, ignore rules, included build-context paths, harness definition, generated Devbox image layer, and build arguments. Source-path changes alone do not cause image rebuilding when effective inputs are identical.

When `open` detects container/image drift, it prints those specific creation reasons and the recreation command before other open output or startup work, then continues immediately. Runtime changes are shown by status, not presented as reasons to recreate in this warning.

Exact container names keep their recorded slots when defaults change. Open targets are arguments to `open`, so target names do not collide with top-level commands. Existing-container start/shell/exec and logs do not load desired configuration. Attached `open`, `shell`, and `exec` forward the invoking terminal's display variables, including `TERM` and `COLORTERM`, without requiring recreation; see [environment precedence](configuration.md#substitution-environment-and-creation-options).

## Errors and next steps

Commands return typed failures with stable codes, safe messages, known targets, and optional `next_steps` argv arrays. Human errors use `Error: <message>` on stderr, with a separate `Target:` line when known and labeled, copyable commands. Codes and operation names stay in JSON rather than the human header. Commands supporting `--json` instead emit one error object on stdout and exit nonzero; success JSON shapes are unchanged. Error fields are `error` (code), `message`, `operation`, optional `target`, `next_steps`, and `related_errors` for joined failures. Interactive `open`, `shell`, and `exec` do not gain a JSON mode, and child output is unchanged. Flag errors do not echo rejected values.

Common codes include `configuration_missing`, `profile_missing`, `harness_required`, `unknown_harness`, `invalid_harness_definition`, `invalid_configuration`, `configuration_unavailable`, `session_missing`, `session_exists`, `create_stop_failed`, `ambiguous_target`, `container_missing`, `session_busy`, `ownership_mismatch`, `container_mismatch`, `managed_config_conflict`, `recovery_unavailable`, `pending_transfer`, `transfer_failed`, `docker_unavailable`, and `docker_inventory_unavailable`. Other Docker failures use `docker_command_failed`; unclassified errors use `command_failed`. Cancellation/deadline failures remain nonzero. Docker/child exit status is preserved even when cleanup also fails.

Next steps retain an explicit `--home`, use exact known targets, and never execute automatically. Each command is labeled with its reason; `Then` marks a sequence and `Or` marks an alternative. The recommended action comes first. Missing-environment guidance offers `open --create` first, then standalone `create`. No `--force` action is suggested by default. If a failure requires manual repair, the message identifies what needs attention rather than inventing a repair command. Error reporting does not initialize homes, repair records, or adopt containers.

## Completion

`completion bash|zsh|fish|powershell` prints a shell completion script. Load it in your shell to complete command/flag names, recorded session targets, live container targets, profiles, valid harnesses, transfer slots, and fixed option values. Folder completion remains available where folders are accepted. For Bash, with the executable on `PATH`:

```sh
source <(devbox-neo completion bash)
```

The loaded script registers both `devbox-neo` and `dbx`. It does not define `dbx` or change an existing alias/function; completion invokes that shortcut with its existing flags. Regenerate and reload previously saved scripts to pick up this registration. With command-name-based Bash/Fish autoloading, also install the generated script under the `dbx` completion filename (`dbx` / `dbx.fish`), or source it at shell startup. Zsh's generated autoload header covers both names.

Suggestions honor `--home` / `DEVBOX_HOME`. Completion does not initialize state, create locks, or modify Docker resources. Container suggestions use a bounded, installation-filtered Docker inventory; unavailable Docker quietly leaves folder completion available. Session suggestions use existing session directories, including corrupt records so they can still be addressed. Invalid harness overrides are not suggested and never fall back to the built-in.

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

## Durable sessions

| Command | Behavior |
|---|---|
| `session list [--sort name\|last-active] [--json]` | Durable session table with harness, profile, activity, container state, and folder; missing containers and corrupt records remain visible |
| `session show <target> [--json]` | Recorded contract and live leases without desired resolution |
| `session reset <target...> [--harness NAME\|--all-harnesses]` | Reset stopped/absent, idle session stores; preserve declared history |
| `session reset --all [--include-history] [--dry-run] [--json]` | Complete-set preflight; include-history clears all selected environment-store contents |
| `session clone <source> <destination-folder> [--profile NAME] [--dry-run] [--json]` | New session ID; stopped/absent source, stopped destination |
| `session relocate <source> <destination-folder> [--dry-run] [--json]` | Preserve ID and running/stopped intent; remove source after destination commitment |
| `session clone\|relocate <folder> --from SLOT --to SLOT [--dry-run] [--json]` | Exact same-folder slots; each slot is a profile name or `.project` |
| `session delete <target...> [--dry-run] [--json]` | Exact state deletion only after containers are gone; remove only verified session image tags |
| `session prune --orphaned [--older-than DURATION] --dry-run` | Preview filtered state cleanup |
| `session prune --orphaned [--older-than DURATION] --yes` | Confirm filtered state cleanup; age is rechecked while locked |

`session list` shows `NAME`, `HARNESS`, `PROFILE`, `LAST ACTIVE`, `CONTAINER`, and `FOLDER`. `CONTAINER` is a live cross-reference (`running`, `stopped`, or `missing`), not a session lifecycle state. Default ordering is by name; `--sort last-active` puts newest activity first, breaks ties by name, and puts unknown activity last. JSON uses the same ordering. Stopped/missing container rows are subdued on supported terminals, with error and pending-transfer details below the table. `--older-than` and `--orphaned` belong only to `session prune`, not listing.

Reset never clears managed auth or shared caches. Store roots remain available for existing Docker bind mounts; the next normal open restores desired managed config. Session delete has no `--all` or age filters. Transfers require idle endpoints, an unused destination, and portable harness declarations for all copied state. Destination configuration controls creation; no workspace files or container-layer changes are copied. Cross-folder transfers retain the source slot unless clone selects another profile. Project destinations must be initialized. Pending transfers block ordinary mutations, including forced deletion; retry the same command to resume. Before commitment, destination inputs must still match the journal. After commitment, retry uses the recorded destination rather than desired configuration.

For numbered configuration menus and scoped config `--show [--json]`, see [configuration](configuration.md#configuration-owner-commands).
