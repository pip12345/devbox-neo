# Output and errors

## Interactive menus

| Key | Action |
|---|---|
| Up/Down or `j`/`k` | Move through the list |
| Enter | Open the selected menu, edit a field, or run an action |
| Esc or `q` | Go back; first clears an active filter |
| Left/Right | Switch between browser objects and application actions |
| Tab | Switch Sessions/Configs while browsing |
| `/` | Filter the current list |
| Page Up/Down | Scroll details |
| `n`, `r`, `a` | Create, Refresh, All-session operations in the browser |
| Ctrl-C | Exit menus; interrupt a foreground operation without closing the browser |

Text fields use Esc to cancel; `q` and `:back` are literal text there. Confirmations start on **No** and require Enter. SIGTERM cancels the whole command.

Successful operation forms close; failed forms keep their inputs. Completed edits stay saved when you leave. Configs already created remain saved if you cancel a session draft.

Session rows and previews show relative **Last active**, not exclusively last opened. `*` marks the folder default. Narrow layouts show the active pane or put activity beneath the name.

Without full terminal input/output, bare browsers show help. Other redirected/dumb-terminal prompts use numbered choices: `0`/`q` goes back and `:back` cancels text entry.

## Environment listings

| Column / marker | Meaning |
|---|---|
| NAME | Folder-local session name |
| FOLDER | Workspace; global list only |
| DEFAULT `*` | Selected folder default |
| HARNESS | Recorded coding tool |
| LAST ACTIVE | Recorded Devbox activity, not filesystem activity |
| CONTAINER | Running, stopped, or missing; `!` indicates an error, `*` a pending transfer |
| LIFETIME | `automatic` or `until stop`, independent of current state |
| CONFIGS | Selected configs, in order |
| FULL NAME (`--wide`) | Exact session/container identifier |

Wide output also includes last action and exact UTC timestamps. `--sort folder` orders folders then names; `name` orders full names; `last-active` is newest first. Sorting applies to the whole command-line table.

Missing containers remain listed. Corrupt records show diagnostics; managed containers without records are reported separately.

## Status classifications

| Result | Meaning |
|---|---|
| No changes | Compared local inputs match |
| Runtime changes | Launch settings, hooks, docs, or managed files changed |
| Recreate needed | Container settings changed |
| Rebuild + recreate needed | Image inputs changed; Recreate also builds |
| Cannot check | A config/state problem prevents comparison |

Status compares local inputs, not available upstream releases. A missing container and invalid configuration are separate conditions. Single-session status shows detailed reasons and relevant next commands.

## Inventory and status JSON

| Command | Shape |
|---|---|
| `list --json` | `sessions` and `unmatched_containers` arrays |
| `status --json` | Same inventory shape, with change diagnostics |
| `status <target> --json` | One status object, plus `record` and `active` details |

| Common field | Meaning |
|---|---|
| `name`, `local_name` | Full and local session names |
| `default` | Matches the saved folder default |
| `manual_start` | Keep-running intent |
| `sources` | Desired config references |
| `desired_change`, `pending_input_changes` | Configuration differences |
| `config_error`, `error` | Available diagnostics |
| `pending_transfer` | Reserved transfer endpoint information |
| `default_errors` | Bulk map of folder-default errors |
| `default_error` | Single-session default error |

Bulk arrays are present when empty. Bulk rows omit full records and leases. Pending endpoints without a record remain inspectable; `record` is omitted and `active` is empty. Per-row errors do not fail bulk status, but unavailable inventory/Docker does.

### Input-change entries

Entries contain `scope`, `code`, and `field`; optional fields are `key`, `path`, `before`, and `after`.

- Scope: `image`, `container`, `runtime`.
- Code: `value_changed`, `input_changed`, `entry_added`, `entry_removed`, `order_changed`, `file_added`, `file_removed`, `file_content_changed`, `file_kind_changed`, `file_mode_changed`.

Public values may appear before/after. Env changes show variable names, never values or hashes. File contents are not printed.

## Configuration output

`edit <target> --show` reports combined settings and contributing configs; folder targets need `--name`. Lists retain per-entry provenance in `trace.entry_sources`. JSON preserves value types; env values are redacted.

Directory editors show one config over built-in defaults. Combined configuration is read-only. Save receipts point to Status; saving a config does not itself apply container changes.

## Deletion results

Session deletion JSON contains `containers`, `sessions`, `retained_sessions`, `dry_run`, and `cancelled`. Use explicit `--container` or `--session` with `--json`.

`config delete <name> --force --json` returns `path` and `deleted`. Blocked deletion reports known users in `next_steps`; incomplete usage information remains an error.

## Errors and next steps

Human errors go to stderr with an explanation, target, and suggested commands. Suggestions never execute automatically. **Then** means a sequence; **Or** means an alternative.

JSON-capable commands emit an error object on stdout and exit nonzero:

| Field | Meaning |
|---|---|
| `error` | Error code |
| `message` | Safe explanation |
| `operation`, `target` | Failed operation and known target |
| `next_steps` | Commands as argv plus reasons |
| `related_errors` | Additional failures |

Open, Shell, Exec, and SSH pass child output through without JSON wrappers. Child/Docker exit status is preserved; cancellation and deadlines are nonzero. Suggested commands retain an explicit home and exact transfer targets.

## Color

`NO_COLOR`, redirected output, or `TERM=dumb` disables human styling. State markers and JSON structure remain available without color.
