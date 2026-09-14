# Output and errors

## Environment listings

`list` shows `NAME`, `HARNESS`, `PROFILE`, `LAST ACTIVE`, `CONTAINER`, and `FOLDER`. `--wide` adds the last action and exact UTC activity/container-creation timestamps.

- `--sort name` is the default.
- `--sort last-active` is newest first, then name; unknown activity sorts last.
- `!` marks an error; `*` marks a pending transfer.
- Stopped/missing rows are dimmed where supported; diagnostics remain readable.

Both `list` and `status --all` report installation-managed containers without session records separately. Corrupt records remain session rows with diagnostics. Listings do not adopt, delete, or repair resources.

## Status classifications

Container state and configuration health are independent. A missing container is not automatically a configuration error.

| Change | Meaning |
|---|---|
| `No changes` | Compared local inputs match |
| `Runtime changes` | Launch, hook, docs, or managed-file inputs changed |
| `Recreate needed` | Container inputs changed |
| `Rebuild + recreate needed` | Image inputs changed; ordinary recreate builds them |
| `Cannot check` | Invalid config/record, ownership mismatch, pending transfer, or another diagnostic prevents comparison |

Single-target status includes saved session ID, harness, image, active-command count, and detailed reasons. Bulk status shows `NAME`, `CONTAINER`, and `CHANGE`, with reasons beneath affected rows. Local file/settings comparisons do not detect newer upstream releases.

`open` prints image/container change reasons before startup and continues with recorded creation settings. Runtime-only changes are not presented as reasons to recreate.

## Inventory and status JSON

| Command | Shape |
|---|---|
| `list --json` | Object with `sessions` and `unmatched_containers` arrays |
| `status --all --json` | Same inventory shape, enriched with desired-change diagnostics |
| `status <target> --json` | One status object, plus `record` and `active` details |

Bulk arrays are present even when empty. List session order follows `--sort`. Status fields include `desired_change`, `pending_input_changes`, `config_error`, `error`, and `pending_transfer` where applicable. Bulk rows omit full records and leases.

An exact pending-transfer endpoint remains inspectable without a session record: `record` is omitted and `active` is empty. Pending transfers skip desired-config comparison. Per-row diagnostics do not fail bulk status; unavailable inventory/Docker does.

### Input-change entries

Each `pending_input_changes` entry contains `scope`, `code`, and `field`, with optional `key`, `path`, `before`, and `after`.

| Field | Values |
|---|---|
| `scope` | `image`, `container`, `runtime` |
| `code` | `value_changed`, `input_changed`, `entry_added`, `entry_removed`, `order_changed`, `file_added`, `file_removed`, `file_content_changed`, `file_kind_changed`, `file_mode_changed` |

Public scalar changes may show before/after values. Environment changes show variable names, never values or hashes. File contents are not printed.

## Configuration output

`config --show` reports effective settings, participating/excluded layers, artifact winners, and harness origin. Lists show per-entry sources; nested fields use dotted names such as `vscode.extensions`. Human output wraps at up to 80 columns or the narrower terminal width.

JSON preserves structured values. `trace.entry_sources` gives layer names in resolved-list order, including duplicates. Env values are redacted; variable references are reported separately. A sparse configuration can be inspected before selecting a harness.

Config menus label sources as `default`, the edited layer, or `inherited - global` / `inherited - profile`. Their overview includes inherited values; editors change only the selected layer.

## Deletion results

Deletion JSON contains `containers`, `sessions`, `retained_sessions`, `dry_run`, and `cancelled`. Explicit `--container` or `--session` scope is required with `--json`.

## Errors and next steps

Human failures go to stderr with a short `Error:` message, optional `Target:` context, and labeled next commands. Suggestions never execute automatically. `Then` marks a sequence; `Or` marks an alternative.

Commands supporting `--json` emit one error object on stdout and exit nonzero:

| Field | Meaning |
|---|---|
| `error` | Stable error code |
| `message` | Safe human-readable explanation |
| `operation` | Failed operation |
| `target` | Target when known |
| `next_steps` | Suggested commands as structured argv and reasons |
| `related_errors` | Additional joined failures |

Common codes include `invalid_configuration`, `configuration_unavailable`, `harness_required`, `profile_missing`, `session_missing`, `session_exists`, `ambiguous_target`, `container_missing`, `session_busy`, `ownership_mismatch`, `container_mismatch`, `managed_config_conflict`, `recovery_unavailable`, `pending_transfer`, `transfer_failed`, and `docker_unavailable`.

Child output is passed through. `open`, `shell`, `exec`, and `ssh` do not provide JSON wrappers. Child/Docker exit status is preserved even if cleanup also fails; cancellation and deadlines remain nonzero. Flag errors do not echo rejected values.

Suggested commands retain an explicit `--home` and exact known targets. Generic creation hints use `create <folder>` with normal configuration selection rather than replaying invocation flags. Missing-environment hints preserve the folder spelling you entered. Transfer retries retain recorded endpoint selectors.

## Color

Human styling is disabled for non-terminal output, `NO_COLOR`, or `TERM=dumb`. This does not change input rules or JSON structure.
