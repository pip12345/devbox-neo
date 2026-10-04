# Output and errors

## Interactive menus

| Key | Action |
|---|---|
| Up/Down or `j`/`k` | Move through the list, wrapping at either end |
| Enter | Open the selected menu, edit a field, or run an action |
| Esc or `q` | Go back; first clears an active filter |
| Right / Left | Open the highlighted object's menu / return to the browser |
| Tab | Switch Sessions/Configs while browsing |
| `/` | Filter the current list |
| Page Up/Down | Move a visible page in a list; scroll a page in read-only views |
| Home/End | First/last list item; top/bottom of a read-only view |
| Ctrl+Page Up/Down | Scroll long context/details without moving the menu selection |
| `n`, `r`, `a`, `b` | Create, Refresh, All-session operations, Browser actions while browsing |
| `c`, `o`, `s`, `r`, `i`, `l`, `e` | Continue, Open, Shell, Recreate, Status, Logs, Exec in a session menu |
| Ctrl-C | Exit menus; interrupt a foreground operation without closing the browser |

The browser's right pane previews the highlighted object's actions. Enter or Right opens that menu; highlighting never runs an action. Browser-wide commands stay separate under **Browser actions** (`b`). Action labels share one style regardless of whether they open another screen. Destructive actions retain red warning text. Categories have a blank line between them. Shortcuts are shown beside actions and do not fire while typing or filtering. In the Recreate form, `r` opens the existing confirmation; it never approves it.

Text fields use Esc to cancel; `q` and `:back` are literal text there. Confirmations start on **No** and require Enter. SIGTERM cancels the whole command.

Successful operation forms close; failed forms keep their inputs. Text edits prefill source values; conflicts reload current values rather than retry. Sensitive input is masked. Completed edits stay saved when you leave. Configs already created remain saved if you cancel a session draft.

Session rows and previews show relative **Last active**, not exclusively last opened. `*` marks the folder default. Narrow layouts show the active pane or put activity beneath the name.

Without full terminal input/output, bare browsers show help. Other redirected/dumb-terminal prompts use numbered choices: `0`/`q` goes back and `:back` cancels text entry. Plain text prompts show non-sensitive current values as context; blank input submits an empty replacement.

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
| SESSION | Exact session directory name accepted by commands |
| CONTAINER NAME (`--wide`) | Docker name; independent of session storage |

Wide output also includes last action and exact UTC timestamps. `--sort folder` orders folders then names; `name` orders local names, then folders; `last-active` is newest first. Sorting applies to the whole command-line table.

Missing containers remain listed. Corrupt records show diagnostics; managed containers without records are reported separately.

## Status classifications

| Result | Meaning |
|---|---|
| No changes | Compared local inputs match |
| Runtime changes | Launch settings, hooks, docs, or managed files changed |
| Recreate needed | Container settings changed |
| Rebuild + recreate needed | Image inputs changed; Recreate also builds |
| Cannot check | A config/state problem prevents comparison |

Status identifies workspace and local name together and compares local inputs, not available upstream releases. Public-setting changes show values; managed-tree and build-context changes show category reasons, not exact filenames. Missing runtime and invalid configuration are separate conditions. Status reports missing images without treating them as corrupt state or replacing a healthy container. Single-session status shows reasons and relevant next commands. Active commands include their action, host PID, and attachment start time.

## Inventory and status JSON

| Command | Shape |
|---|---|
| `list --json` | `sessions` and `unmatched_containers` arrays |
| `status --json` | Same inventory shape, with change diagnostics |
| `status <target> --json` | One status object, plus `record` and `active` details |

| Common field | Meaning |
|---|---|
| `target` | Exact session directory name; unmatched-container rows use their Docker name |
| `session_id`, `local_name` | Internal stable ID and editable session name |
| `container_id`, `container_name` | Applied Docker instance and its name |
| `image_missing` | Recorded image ID is absent; existing container access is unaffected |
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

Public values may appear before/after. Env changes show variable names, never values or hashes. Managed content and build contexts use `input_changed` category entries. Dockerfiles and ordered hooks retain specific content/order reasons. File contents are not printed.

## Configuration output

`edit <target> --show` reports combined settings and contributing configs; folder targets need `--name`. Lists retain per-entry provenance in `trace.entry_sources`. JSON preserves value types; env values are redacted.

`config show` and directory editors show one config over built-in defaults. Combined configuration is read-only. Save receipts point to Status; saving does not itself apply container changes.

| JSON command | Fields |
|---|---|
| `edit <target> --config REF --json` | `name`, `workspace`, ordered `sources`, `next_steps` |
| `config users <name\|path> --json` | `path`, `users` (session directory names), `complete` |

Incomplete usage scans exit nonzero with known users in `partial_result` and `complete: false`.


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
| `partial_result` | Completed deletions or an incomplete config-usage report |

Open, Shell, Exec, and SSH pass child output through without JSON wrappers. Child/Docker exit status is preserved; cancellation and deadlines are nonzero. Suggested commands retain an explicit home and exact transfer targets.

## Color

`NO_COLOR`, redirected output, or `TERM=dumb` disables human styling. State markers and JSON structure remain available without color.
