# Graphical menu inventory

Scouting inventory of the current `dbx` terminal UI, including the contextual
browser, compact session layout, styling, and local-shortcut follow-up to
`62fe4d5`.
This is an inventory for UI work, not a redesign or a user guide. It covers
reachable graphical menus, forms, pickers, read-only screens, confirmations,
and their conditional states. Runtime-dependent lists are described by their
contents and rules rather than by every possible session/config name.

The sketches are **schematic, not terminal captures**. Long menus show their
complete logical contents; the real UI scrolls them within the terminal.
Unless stated otherwise, a nested screen uses the shared workflow layout below
and has Back/Esc navigation. Back is a keyboard action, not an extra menu row.

## Contents

1. [Entry points](#1-entry-points)
2. [Shared layouts, controls, and states](#2-shared-layouts-controls-and-states)
3. [Sessions browser and folder menu](#3-sessions-browser-and-folder-menu)
4. [Session menu](#4-session-menu)
5. [Create session](#5-create-session)
6. [Manage configs and config-chain pickers](#6-manage-configs-and-config-chain-pickers)
7. [Configs browser and config creation](#7-configs-browser-and-config-creation)
8. [Config settings editors](#8-config-settings-editors)
9. [Session operation forms](#9-session-operation-forms)
10. [Copy or move](#10-copy-or-move)
11. [All-session operations and deletion](#11-all-session-operations-and-deletion)
12. [Read-only views and help](#12-read-only-views-and-help)
13. [Migration gate](#13-migration-gate)
14. [Text-entry and confirmation catalog](#14-text-entry-and-confirmation-catalog)
15. [Navigation routes and keystrokes](#15-navigation-routes-and-keystrokes)
16. [Coverage and source map](#16-coverage-and-source-map)

## 1. Entry points

Graphical presentation requires terminal input and output and `TERM` other
than `dumb`. Migration presents on stderr. Some direct workflows can also use
plain numbered prompts; those are not separate graphical menus.

| Entry | Graphical destination / condition |
|---|---|
| `dbx` | Sessions browser |
| `dbx config` | Configs browser |
| `dbx edit <folder>` without `--name` | Main Sessions browser, with that folder visible and focused; not a separate folder/default picker |
| `dbx edit <session-id>` or `dbx edit <folder> --name NAME` | Manage configs, unless an explicit non-menu operation flag is supplied |
| `dbx create <folder>` | Create session when name or configs are missing; supplied inputs prefill the form |
| `dbx config create [name\|path]` | Create config, when interactive and no direct setup/JSON flags are supplied |
| `dbx config edit <name\|path>` | Config settings editor, when interactive and no direct setup/JSON flags are supplied |
| `dbx delete <targets>` or `dbx delete <filters>` without an explicit scope | Deletion form after successful preflight; supplied targets/filters prefill it. Bare `dbx delete` rejects the missing selection |
| `dbx config delete <name>` without `--force` | Config-deletion confirmation after safety checks |
| Any state-backed entry needing migration | Migration required gate before the requested action |

Bare `dbx` / `dbx config` show help rather than a browser without a graphical
terminal. Supplying explicit command options can bypass forms: for example,
complete `create` inputs, `edit --config`, or `delete --container/--session`.
Open, Start, Shell, Exec, SSH, logs, network commands, status, copy and rename
are not themselves automatic launchers for their browser forms.

## 2. Shared layouts, controls, and states

### Layout A: browser, wide terminal

```text
Devbox Neo    [ Sessions ]    Configs     Tab <->
[b] Browser actions  [n] Create session  [a] All sessions  [r] Refresh

Sessions       State   Last active | Main · pi · stopped · automatic
----------------------------------| ------------------------------
  project                         | Folder      /work/project
>   Main *     stopped  2 hours ago| Last active 2 hours ago
    Test       running just now   |
                                  | Use        Continue         [c]
                                  |            Open             [o]
                                  |            Open with options
                                  |            Shell            [s]
                                  |            Exec             [e]
                                  |            SSH
                                  | ...remaining contextual actions...
<notice>
------------------------------------------------------------------
Up/Down browse  Enter/Right actions  Esc exit  Ctrl-C exit
```

Objects are on the left; actions for the highlighted object are previewed on
the right. Configs use the same frame without state/activity columns and
preview Edit config. Highlighting never invokes a handler. Enter or Right
opens that object's interactive menu. Browser-wide commands are separate in
the header shortcut strip and the Browser actions menu (`b`). The strip is
clipped on small terminals, with Browser actions first so all commands remain
accessible. Its actual All-session operations label may be truncated.

### Layout B: nested workflow / form / picker

```text
Devbox Neo    [ Sessions ]    Configs

Sessions                          | <screen title>
----------------------------------| -----------------------------
  project                         | <context fields/body>
    Main *  stopped  2 hours ago  |
                                  | > <action or editable field>
                                  |   <action>          <value>
                                  |   [ ] <toggle>
                                  |
                                  | <selected action detail>
<notice>
------------------------------------------------------------------
Up/Down move  Enter select  Esc back  Ctrl-C exit
```

- The left pane is retained navigation, not another simultaneously editable
  menu. Left returns from a nested workflow when it has navigation.
- Nested screens with inherited navigation retain the Sessions/Configs header,
  but Tab switches browsers only at the browser itself. Those nested tabs are
  not active controls.
- A direct form without inherited navigation is full-width and has no tabs.
- Compact session facts appear beside the title, wrapping onto another line
  when needed. Context is above the action list. Selected-action help,
  values/provenance, or a blocked reason is below it. Group labels use a gutter
  instead of separate heading rows, with one blank row between categories.
  Long lists show more-above/below hints.
  Ctrl+Page keys scroll long context/details without changing selection.
- Hidden actions are absent. Blocked actions remain selectable: their reason
  appears in the detail area, and Enter reports it without invoking the action.
  They are not represented as a separate disabled-button widget.

### Layout C: read-only view

```text
Devbox Neo

<view title>
------------------------------------------------------------------
<text, table, or pretty-printed JSON>
<more scrollable output>

------------------------------------------------------------------
Up/Down scroll  Enter/Esc back  Ctrl-C exit
```

Read-only views have no action rows. They retain the navigation pane when
opened inside a browser at a sufficient width. This sketch shows the
standalone/narrow form; the footer wording is illustrative.

### Layout D: text entry

```text
Devbox Neo

Enter a value
------------------------------------------------------------------
<prompt for this field>

> <prefilled editable text>|

<context or validation feedback>
------------------------------------------------------------------
Enter accept  Esc cancel  Ctrl-C exit
```

Text entry retains inherited navigation. Sensitive inputs are masked. Existing
control characters cause an explicit JSON-string editing mode, preserving
escapes instead of silently removing characters. Rejected input stays in the
field for correction. Sensitive validation errors use a generic message.
`q` and `:back` are literal input in the graphical field; Esc cancels it.

### Layout E: confirmation

```text
Devbox Neo

Confirm action
------------------------------------------------------------------
<exact targets, consequences, and question>

> No - keep unchanged
  Yes - proceed
------------------------------------------------------------------
<selection / cancellation keys>
```

This replaces the navigation frame, rather than appearing as a small overlay.
No is initially selected. Left selects No; Right selects Yes. Up/Down also
move between them, and Enter is required. Merely typing `y` does not approve.
Esc declines. Actual labels use an em dash. The answer/cancellation is also
written to the normal terminal after leaving the graphical screen.

### Responsive and transient states

| State | What is shown / what changes |
|---|---|
| Wide, at least 88 columns | Two panes when a nonempty collection/navigation exists; content width is terminal width minus four columns |
| 48–87 columns, at least 20 rows | One pane: object list in the browser; object's menu after Enter/Right; Left returns from nested navigation |
| Narrow navigation rows | Last-active text moves beneath the session row when state/activity columns would leave too little name space |
| Below 48 columns or 20 rows | `Devbox Neo`, `Enlarge to at least 48 × 20.`, `Ctrl-C exits.` instead of menu content |
| Empty collection | No left pane; empty-state message plus application actions; focus starts on the first action |
| Filter editing | Text input replaces the search/detail area; matching rows update as text changes |
| Filter applied | `/ <query>` is visible; Enter dispatches the displayed matched item/action |
| No matches | `No matches`; no matching item/action can be submitted |
| Working | Spinner plus `Working…`; footer becomes `Ctrl-C cancels`; repeated action submission is ignored |
| Notice/error | First line near the footer; multiline notices also appear above preview/workflow context, with command guidance if supplied |
| Foreground handoff | Alternate screen is left; child input/output owns the normal terminal |
| Foreground finished/failed | Results/errors stay in the normal terminal with `Press Enter to return to the menu…`; state refresh follows |
| Foreground refresh failed | Menu notice says the displayed inventory may be stale |
| No color | Same layout and state markers; `NO_COLOR` disables styling without adding menu-depth hints |
| Saved receipts | Printed to the normal terminal when the runner finishes; not a separate success dialog |

ASCII notation here: `>` = focused row, `*` = selected/default, `[ ]` / `[x]`
= off/on toggle. Actual widgets use `▸` for focus, `›` for retained navigation,
and `○` / `✓` for toggles. All action labels share the same bold weight.
Navigation depth has no special color, font treatment, or text marker.
Destructive workflows retain red warning text, focus uses a background, and
status/warnings retain their colors. Group gutters and category gaps are
not selectable. Shortcuts are muted and aligned in a compact adjacent column;
blocked rows also show `blocked`. There are no menu-depth punctuation suffixes.

### Shared keys

| Key | Meaning |
|---|---|
| Up/Down or `k`/`j` | Move, wrapping at list ends; scroll text if there are no choices |
| Enter | Open/select/submit; leave an actionless view |
| Esc or `q` | Clear an applied filter first; otherwise Back/Exit |
| Left/Right | Left backs out of nested navigation; Right opens the highlighted object's menu; No/Yes in confirmations |
| Tab | Switch Sessions/Configs only in the root browser |
| `/` | Start filtering the active list; unavailable for confirmation, text entry, or a list with no matches |
| Enter / Esc while filtering | Finish filtering / clear filter |
| Home/End | First/last matched row; top/bottom of a read-only view |
| PgUp/PgDn | Move a visible page through matched rows, clamping at endpoints; scroll read-only views by a page |
| Ctrl+PgUp/PgDn | Scroll long context/details independently of list selection |
| `n`, `r`, `a`, `b` | Browser shortcuts; `b` opens the full Browser actions menu |
| `c`, `o`, `s`, `r`, `i`, `l`, `e` | Session-menu Continue, Open, Shell, Recreate, Status, Logs, Exec; not active while typing/filtering |
| Ctrl-C | Exit/cancel menus; while a foreground operation owns the terminal, interrupt that operation without cancelling the browser |
| SIGTERM / SIGHUP | Cancel the whole command; normal attachment cleanup runs |

## 3. Sessions browser and folder menu

**Owner:** `frontend.go`: `browse`, `sessionCollection`, `sessionFields`, `folder`.

### Sessions browser

Layout A previews the highlighted folder/session's actual menu actions.
Press `b` for **Browser actions**, containing the complete browser-wide list:

```text
Browser actions
-------------------------------
  Create session [n]
  All-session operations [a]
  Open folder by path
  Sort sessions                 name
  Refresh [r]
  Help and shell integration
```

| Object / state | Preview and behavior |
|---|---|
| Folder | Full path as title; `Default` (name or None), `Sessions` count, optional default `Error`; Enter opens folder menu |
| Session | Name/title with harness, container state, and lifetime; `Folder`, `Last active`, optional error/transfer warning; session action preview; Enter/Right opens session menu. IDs/container names are in Status; config references are in Selected configs |
| Default session | `*` after its list name; no redundant Default field in its preview |
| Pending transfer | List state `transfer pending`; preview adds `Transfer: Pending — use Copy or move` |
| Inventory error on a row | List state `error` (takes precedence over transfer state); preview adds `Error` |
| Missing container | `missing`, not automatic creation/recovery |
| Existing container | `running` or `stopped` |
| Lifetime | `automatic`, `until stop`, or `-` without a durable session ID |
| Broken/uncommitted record | Remains visible, possibly outside folder grouping; same object-menu entry path |
| Unmatched managed container | Separate unindented object with error details; not a fabricated saved session |
| Empty inventory | `No sessions yet.`; first action is Create session |
| Failed inventory | Notice `Inventory unavailable: ...`; empty preview `Session inventory unavailable.`; config navigation remains available |
| Bad folder default | Warning plus folder Error field; not silently replaced |

Folders sort by full path; their displayed names are the shortest unique path
suffixes. Full paths remain in preview/search. Sessions are indented beneath
their folder. An explicitly opened empty folder remains visible. On initial
entry, the first displayed session in the current canonical folder is focused
if one exists; this does not change the default. Inventory is not polled. The empty browser displays the global command list
(including Browser actions) in place of a contextual preview.

`Open folder by path` in Browser actions opens a Workspace folder text input. `Sort sessions`
opens this picker (sorting applies within folders):

```text
Sort sessions within folders
----------------------------
> Name
  Last active
```

This is a plain picker, not a current-selection-marked picker. Default sort
is `name`; choosing Last active uses newest-first ordering.

### Folder menu

```text
/work/project
-------------------------------------------
Default   Main
Error     <only if default lookup failed>

> Create session here
  Clear folder default        <conditional>
```

- Create session here prefills the folder in Create session.
- Clear is hidden only when there is neither a default nor a default error.
- Clearing saves immediately and produces a receipt. There is no confirmation
  or separate default-selection picker. Set a default from a session menu.

## 4. Session menu

**Owner:** `frontend.go`: `session`, `defaultAction`.

Layout B. Continue is first, including immediately after successful creation.

```text
Session · Main · pi · stopped · automatic
------------------------------------------------------
Folder     /work/project

Use        > Continue                 [c]
             Open                     [o]
             Open with options
             Shell                    [s]
             Exec                     [e]
             SSH

Inspect      Status                   [i]
             Logs                     [l]
             Networks

Container    Start
             Stop
             Recreate                 [r]

Manage       Selected configs
             Make folder default       <or Clear folder default>
             Copy or move
             Rename session
             Change workspace
             Delete

<focused action explanation>
<more-above/below hint when the list does not fit>
```

| Action | Destination / immediate behavior |
|---|---|
| Continue / Open | Foreground recorded harness, with/without continuation |
| Open with options | Invocation-options form |
| Shell | Foreground configured shell, no intermediate form |
| Exec / SSH | Their respective forms |
| Status | Read-only detailed status |
| Logs / Networks | Their respective forms |
| Start | Foreground Start immediately; help says Keep running until explicitly stopped |
| Stop / Recreate | Their respective forms |
| Selected configs | Manage configs; detail describes the current config chain and explains this edits references/order |
| Make/Clear folder default | Immediate saved change; label flips to reflect current default |
| Copy or move | Transfer form, or pinned pending-transfer form |
| Rename session | Rename form; successful rename leaves this session menu so the browser can refocus the renamed ID |
| Change workspace | Prefilled text input warning that a matching old-folder default will be cleared; saves reference, shows explicit Recreate guidance |
| Delete | Deletion form; leaves session menu if saved session/incomplete directory is gone |

The default action is hidden when there is no durable session ID. Apart from
that, operations are **not generally hidden/disabled according to container
state**. Running/stopped/missing/error/pending rows keep the menu; shared
services report unavailable or unsafe operations. An uncommitted creation's
Delete form initially selects saved-data deletion instead of container-only.

The menu refreshes facts when rebuilt. If the target disappeared, it reports
that the session no longer exists and asks for browser refresh. Browser preview
and interactive menu share the same action builder; previews contain no
executable handlers. Session facts stay compact, including lifetime. Hotkeys
use the same handlers as row selection. `r` opens Recreate, and `r` in that
form opens its confirmation; neither selects Yes or bypasses the warning.

## 5. Create session

**Owner:** `session_create.go`, `frontend.go`: `createSessionFromDraft`.

```text
Create session
---------------------------------------------------
Folder   /work/project
Name     Not set
Configs  None
Harness  <when configs resolve>
Error    <when selected configs fail to resolve>

> Set session name              <or Change session name>
  Add existing config
  Create config
  Replace config                <if any selected>
  Remove config                 <if any selected>
  Reorder configs               <if at least two>
  Change folder
  [ ] Make folder default       <Replaces Main, if set>

  Create session
```

- Name input is prefilled when supplied; name validation stays in the input.
- Config actions edit a local draft; creation may remove its last config.
- Change folder is prefilled and validates/canonicalizes the new workspace.
- Make folder default starts off unless explicitly supplied by the CLI.
  Existing selection is identified as `Replaces <name-or-ID>`; an unreadable
  default is shown as `Cannot read current default: ...` instead.
- Submit is blocked with one of: `Set a session name and add at least one
  config first.`, `Set a session name first.`, or `Add at least one config
  first.` It does not pre-disable every possible service-side error.
- Cancel abandons the session draft. Configs created through its child workflow
  are already saved independently and are not removed.
- Resolve/build errors retain the populated form for retry. Materialization
  runs in the normal terminal with output acknowledgement.
- Success opens the saved, stopped session menu. If the record committed but
  final Stop/default selection failed, it opens that saved session with repair
  guidance instead of offering to recreate the same session from scratch.

## 6. Manage configs and config-chain pickers

**Owners:** `source_menu.go`, `source_picker.go`.

### Manage configs

```text
Manage configs
---------------------------------------------------
Session: Main
Folder: /work/project
Configs, in order:
  1. base          /home/me/.devbox-neo/configs/base
  2. ./project     /work/project/project
  <per-source error and repair command, if needed>
Configuration error: <if combined config is invalid>

> Add existing config
  Create config
  Replace config                <if any selected>
  Remove config                 <if any selected>
  Reorder configs               <if at least two>
  Show combined configuration
```

The five chain actions are shared with Create session. Here each accepted
change saves immediately, without applying it to the container. Back does
not undo completed edits. Removing the sole remaining config is blocked:
`Keep at least one config; use Replace config instead.` Invalid combined
settings do not prevent repairing the chain. This screen does not open a
nested settings editor for an existing config.

| Chain action | Interaction |
|---|---|
| Add existing config | Existing-config picker, append accepted selection |
| Create config | Shared Create config workflow; append newly created config |
| Replace config | Select a config in the current chain, then existing-config picker with current selection marked |
| Remove config | Select a config in the current chain, then save removal |
| Reorder configs | Select a config, then Choose the new position |
| Show combined configuration | Separate Back-only read-only view |

Duplicates/invalid references report an error without applying the proposed
chain. A concurrent source-chain change reloads current saved references and
reports the conflict rather than overwriting them. Saved-change receipts
point to Status; they do not claim container settings were applied.

### Existing-config picker

```text
Select an existing config
------------------------------------------------------------
Current selection: base (fixed)          <replacement only>

  NAME           TYPE       PATH
Named configs
> base           fixed      /home/me/.devbox-neo/configs/base
  tools          fixed      /home/me/.devbox-neo/configs/tools

Workspace configs
  ./project      relative   /work/project/project

Current-directory configs
  ./local        relative   /work/other/local

  Enter a directory path
```

Rows are grouped in that order and deduplicated by canonical directory.
The current equivalent path is marked `*` for replacement. Selecting a
suggestion retains its discovery root; typed paths use the invoking directory.
Named and discovered references can therefore have different reference types.
Discovery failures are notices; available groups and typed paths still work.
The chooser validates the selected path when accepted; it does not promise
that every suggested config will produce a runnable session.

Empty candidate variant:

```text
Select an existing config
--------------------------------
No configs found.

> Enter a directory path
  Create and add config
```

Create and add config is visible only in the empty picker. The parent chain
still always has its separate Create config action. Directory input for a
replacement is prefilled from its current reference.

### Chain selection and reorder pickers

```text
Select a config                  Choose the new position
----------------------------     ----------------------------
> base (fixed)                   Current selection: Position 2
  ./project (relative)
                                   Position 1
                                 > * Position 2
                                   Position 3
```

Chain selection lists every selected reference in order, using its label and
kind. Position choices are `Position 1` through `Position N`, with the current
position marked/focused. All child pickers can return without changing the
chain.

## 7. Configs browser and config creation

**Owners:** `frontend.go`, `config_create_flow.go`, `setup_menu.go`.

### Configs browser

```text
Devbox Neo      Sessions    [ Configs ]    Tab <->

Configs                  | base
-------------------------| Location   Named
> base                   | Directory  /home/me/.devbox-neo/configs/base
  ./project              | Harness    pi
                         | Error      <if this entry is invalid>
                         |
                         | Object actions
                         | -----------------------------
                         |   Edit config

[b] Browser actions  [n] Create config  [r] Refresh
```

- Discovery appends local entries first, then named entries. Local/Named is
  shown in preview; there are no selectable group headings in this browser.
- Enter/Right opens Config · `<name-or-path>`. The contextual preview shows
  Edit config, not browser-wide commands. Browser actions (`b`) offers Create
  config, Edit a directory by path, Refresh, and Help and shell integration.
  Editing by path asks for a config name/directory and enters the same editor.
- Empty state: `No configs yet.`; focus starts on Create config.
- Named-inventory and local-discovery failures get distinct notices and do
  not discard entries successfully obtained from the other source.
- Invalid named configs remain visible. Local discovery only offers parseable
  config layers. Config browsing itself does not require loading Docker.

### Create config

```text
Create config
-------------------------------------------
> Name/location       Unset
  Harness             Unset
  Optional files      None

  Create config
```

Name/location can be edited even when supplied by the caller. It rejects
invalid/already-claimed destinations while preserving the prior accepted
choice. Submit is blocked until a destination is accepted:
`Set a config name or directory first.` Harness and files are optional; neither
subscreen is forced. Cancel creates nothing. Create publishes the config
outside the alternate screen; result notices/receipts return to the caller.
A publication error can report files already created; there is no rollback
promise or automatic retry screen for that partially published config.

### Harness picker for creation

```text
Select a harness
--------------------------------
Current selection: Unset

  claude
  opencode
  pi
> * Leave unset
```

Choices come from valid harness definitions, including custom definitions.
The three names shown are the built-ins, not a hardcoded complete universe.
Invalid definitions produce `Unavailable harness ...` notices and are omitted.
A selected harness is marked/focused instead of Leave unset. Back preserves
the accepted choice. With no valid definitions, Leave unset remains available.

### Optional files and harness-file target

```text
Choose optional files
-----------------------------------------------------
Current selection: None

> [ ] Harness config files
  [ ] setup.sh
  [ ] before-open.sh
  [ ] docker/Dockerfile

  Continue
```

Selecting Harness config files opens:

```text
Choose which harness's config files to add
-----------------------------------------------------
Current selection: <current target or None>

> <valid harness name>
  <other valid harness names>
```

There is no Leave unset row in the file-target picker. It prefers the prior
file target, then the draft's selected harness. If none are available, a notice
is shown and the file toggle remains off. Once checked, its label includes
`Harness config files (<harness>)`. Continue accepts the file selection; Back
abandons this child draft. Unchecking harness files clears its target on
Continue. The other three rows are simple toggles.

The same optional-files menu is reached from an existing config editor.
There Continue adds missing files immediately, reports Created/Kept existing,
and does not change the config's harness setting. Continuing with no selected
artifacts changes nothing.

## 8. Config settings editors

**Owners:** `config_menu.go`, `internal/resource/config_edit.go`.

### Config dashboard

```text
Config · base
---------------------------------------------------------
Directory                    /home/me/.devbox-neo/configs/base
Used by saved sessions       <only when nonempty>
<usage/source/effective-config warnings, if any>

> Harness                    pi
  Network                    default
  Shell command              bash
  Harness arguments          None
  Docker options             None
  Mounts                     None
  Environment variables      None
  Port forwards              None
  VS Code extensions         None
  Base image                 <effective image>

  Add optional files
  Delete config              <named configs only>

Value   pi                   <focused scalar's detail>
From    base
```

The order above is the complete editable-field list. These are settings for
one config directory over defaults, **not** the entire selected session chain.
The graphical version uses action/value rows and a focused detail panel, not
the plain prompt renderer's Setting/Value/Source table.

| Label / key | Control | List-entry noun |
|---|---|---|
| Harness / `harness` | Scalar, with harness choices | — |
| Network / `network` | Scalar text | — |
| Shell command / `shell` | List | argument |
| Harness arguments / `harness_args` | List | argument |
| Docker options / `docker_args` | List | option |
| Mounts / `mounts` | List | mount |
| Environment variables / `env` | Sensitive list | environment variable |
| Port forwards / `ports` | List | port forward |
| VS Code extensions / `vscode` | Extension list | extension |
| Base image / `base_image` | Scalar text | — |

Dashboard states:

- Scalars show Value/From. Nonempty lists show numbered values and each entry's
  From provenance in the detail pane. Environment values are redacted.
- Unset, empty list, and malformed field values display `Unset`, `None`, and
  `Invalid value`, respectively; missing effective values during a resolution
  failure display `Unavailable` with unknown origin.
- Config source unreadable/unparseable: settings actions and Add optional files
  are hidden; `Settings unavailable` is shown. Named Delete may remain.
  Direct `config edit` prechecks source readability before opening; entering
  through the browser can show this error dashboard.
- Effective resolution failed but source readable: fields remain editable;
  warning says configured-here values are shown instead.
- Partial usage lookup: `Shared-use report is incomplete` plus known users.
- Delete config exists only for named configs, not arbitrary path configs.
  Its safety preflight may refuse before the confirmation is shown.
- Back is Back when nested, Exit for standalone `config edit`. Each completed
  edit is already saved; there is no global Save/Discard dialog.

### Scalar setting submenu

```text
Network (network)
--------------------------------------------------
<field-specific help>
Configured here: default        <if locally present>

> Edit value
  Remove this setting           <if locally present>
```

The same layout is used for Harness and Base image. Edit starts from the
literal source value (including expressions), not a resolved display value.
Remove this setting removes the local key immediately. It is hidden for an
inherited/unset key.

Network and Base image go directly to text entry. Harness with valid definitions
uses this intermediate picker:

```text
New value
---------------------------------------------
Current selection: <local literal or None>

  claude
  opencode
  pi
  Enter a value or host expression
```

A matching current harness is marked/focused. The final row opens prefilled
text entry. With no valid harness choices, the editor goes directly to text.
There is no Leave unset row here; use Remove this setting in the parent.
This picker uses Cancel navigation.

A generic Yes/No scalar picker exists in `readConfigValue`, but **no current
ConfigFields entry is boolean**, so it is not a reachable production menu.

### List setting submenu

```text
Mounts (mounts)
--------------------------------------------------
<field-specific help>
Mounts configured here:
  1. /data:/data:ro
  2. ./cache:/cache

> Add mount
  Edit mount
  Remove mount
  Remove this setting
```

This template applies to all seven list fields, substituting the exact label,
key, and noun from the table. Only locally configured entries are edited.

| Local state | Actions |
|---|---|
| Key absent | Add `<noun>` only |
| Key present but empty | Add `<noun>`; Remove this setting |
| Nonempty | Add; Edit; Remove; Remove this setting |

Edit and Remove open list pickers:

```text
Select mount                     Remove mount
----------------------------     ----------------------------
> /data:/data:ro                  > /data:/data:ro
  ./cache:/cache                   ./cache:/cache
```

Titles are `Select <noun>` / `Remove <noun>` for each field. Labels redact
sensitive values, show `(empty)` for an empty entry, and escape control
characters. Add/Edit then use `New <noun>` text entry, masked for environment
variables. NUL is rejected. Each accepted operation saves immediately; removing
one entry and removing the whole setting are distinct actions.

Save/validation failures report `Not saved: ...`. Retriable value failures
retain the attempted input; a concurrent-edit conflict returns to refreshed
saved state instead of replaying stale input. Cancellation affects only the
unfinished edit, not prior successful changes.

## 9. Session operation forms

**Owner:** `frontend_operations.go`, plus `rename.go`.

All forms below use Layout B and Back. A successful submit normally returns
to its parent; service errors retain the form and its local inputs. Execution
uses foreground handoff and output acknowledgement where indicated. The
forms are not shown by invoking the equivalent fully specified CLI command.

### Open with options

```text
Open with options
-----------------------------------------------
> [ ] Continue previous conversation
  Harness arguments                  []
  Trailing arguments                 []
  Open
```

Continue starts off; both argument arrays start empty. Argument edits are
invocation-only. Configured harness args precede continuation/one-off args;
trailing args are appended last. Open launches the harness in the foreground.

### Exact argument editors

Three title variants: **Harness arguments**, **Arguments appended last**, and
**Command arguments**. Same content in each:

```text
Command arguments
-----------------------------------------------------
> Argument 1                 <exact value in detail>
  Argument 2
  Add argument
  Set arguments from JSON
```

Argument values are selected-row detail, not inline columns. With no arguments,
only Add argument and Set arguments from JSON appear. Selecting an existing
argument opens:

```text
Argument
----------------------------
> Edit value
  Remove
```

- Edit/Add: text input `Exact argument (empty is allowed):`, prefilled for Edit.
- JSON: prefilled `JSON array of arguments:` input; accepts a string array,
  including `[]` to clear, but not `null`. Escapes preserve tabs/newlines.
- No shell tokenization, reorder action, or save-to-config operation here.
  Returning with Back keeps accepted edits in the parent operation draft.

### Exec, SSH, logs, Stop, and Rename

```text
Execute a command                  SSH sharing
-------------------------------    -------------------------------
> Executable              -        > Destination               -
  Arguments               []         [ ] Use host SSH master
  Run command                        Connect

Docker logs                        Stop container
-------------------------------    -------------------------------
> Tail                    100      > [ ] Force: allow interruption
  [ ] Follow until interrupted             of active commands
  Read logs                          Stop container

Rename session
-------------------------------
> New session name        -
  Rename session
```

| Form | Defaults / blocked / validation / effects |
|---|---|
| Execute a command | Empty executable; Run command blocked with `Enter an executable first.`; executable input rejects empty; arguments use Command arguments |
| SSH sharing | Empty destination, host master off; Connect blocked with `Enter a destination first.`; destination validated by SSH-sharing rules; foreground connection must stay open while shared |
| Docker logs | Tail `100`, follow off; Tail accepts `all` or a nonnegative count; Read logs describes these as Docker logs, not attached harness output |
| Stop container | Force off; submit has no additional confirmation; active-command rejection is an operation error unless permitted by Force |
| Rename session | New name starts empty, not prefilled from old name; submit blocked with `Enter a new session name first.`; name validation; changes label only, not container/history/default |

### Recreate (single or all)

```text
Recreate
--------------------------------------------------
> Inspect pending changes
  [ ] Rebuild image without cache
  [ ] Force container replacement
  Recreate                                        [r]
```

Same title/options for a selected session and Recreate all. Both toggles start
off. Inspect opens single/all Status according to scope. `r` activates the
Recreate submit row through its normal dispatcher. Submit asks:
`Apply current config? Runtime may restart; if replaced, container-local
changes will be lost.` No keeps the form. Yes runs foreground application;
success closes it, failure keeps the selected toggles and refreshed state.

### Networks

```text
Networks
--------------------------------------------------
> Inspect networks
  Environment exports
  Connect network
  Disconnect network
```

- Inspect networks opens **Network facts**, a JSON view.
- Environment exports opens the picker below.
- Connect/Disconnect both open `Existing Docker network:` text input, not a
  discovered-network picker. Empty/whitespace input is rejected locally.
- Accepted network changes run in the foreground. Success closes Networks;
  cancel/error leaves it open.
- The primary-network, host-mode, missing-container, and unattached-network
  restrictions are reported by the service, not by hidden menu rows.

```text
Network environment
--------------------------------------
> All exports
  DEVBOX_DEFAULT_GATEWAY_IP
  DEVBOX_HOST
  DEVBOX_NETWORK
  DEVBOX_PRIMARY_NETWORK
```

Variable rows are sorted alphabetically. All exports opens a same-titled
read-only view of shell `export` lines. Each variable opens a view titled with
its name and containing only its value, not shell syntax.

## 10. Copy or move

**Owner:** `frontend_operations.go`: `transfer`.

```text
Copy or move
----------------------------------------------------------
> Destination folder (empty keeps source folder)       -
  Destination local name (empty preserves name)        -
  [ ] Move: remove source after destination is ready
  Preview
  Transfer
  Abort pending transfer                 <prepare phase only>
```

| State | Fields and available work |
|---|---|
| New transfer | Destination/name empty; Move off (copy); all three fields editable; no Abort |
| Pending preparation | Recorded destination/name/mode displayed; all three input rows blocked with `Pending transfer: recorded endpoints and mode are pinned.`; Preview, Transfer, Abort available |
| Committed pending transfer | Same pinned inputs; Preview and Transfer remain the actual labels; Transfer only finishes cleanup; Abort absent |

Preview opens **Transfer preview**, a pretty JSON result showing endpoints,
mode, rebased sources, and other result fields. Transfer first obtains a
preview, then confirms exact endpoints and warns `Workspace/config files are
not copied.` Failed preview produces an error instead of confirmation.

Abort has its own confirmation: discard the uncommitted destination attempt
and retain the source. It does not require successful config resolution or a
preview. Yes runs foreground Abort and refocuses the source. Copy returns to
the source menu; successful move leaves the old session menu. Failures remain
visible and retry does not authorize changing pinned endpoints.

## 11. All-session operations and deletion

**Owners:** `frontend_operations.go`, `delete.go`.

### All-session operations

```text
All-session operations
-------------------------------
> Status for all sessions
  Recreate all
  Bulk deletion
```

These lead to the all-status view, shared Recreate form, and bulk-deletion form.

### Single / fixed-target deletion

```text
Delete · Main
--------------------------------------------------
Folder    /work/project

> Delete scope           Container only
  Force                  Off

  Preview deletion
  Delete
```

- One readable saved target: name in title and Folder context. Otherwise its
  target string is used in the title.
- Several fixed targets: title `Delete sessions`, context lists `Sessions`.
- Default scope is Container only, except session-menu cleanup of an
  uncommitted creation starts with Container and saved data/history.
- Force is an inline Off/On value, not the checkbox styling used by Stop.
  Its detail says active commands may be interrupted, but deleting saved data
  still requires idle sessions.

### Bulk-deletion form

```text
Delete sessions
-------------------------------------------------------------
> Choose exact targets                         0 selected
  [ ] All saved environments and unmatched managed containers
  [ ] Stopped containers only
  [ ] Missing containers only
  Inactive longer than (empty disables)        -
  Delete scope                                 Container only
  Force                                        Off

  Preview deletion
  Delete
```

Selecting any exact targets clears all filter toggles and age, and hides those
filter rows while targets remain selected. Clearing the target selection
makes filters visible again. With no targets and no filter, both Preview and
Delete are blocked: `Select exact targets or at least one filter.` Filters
intersect, rather than selecting independent union groups. Age accepts a
positive Go duration such as `720h`; empty disables it. There is no free-text
exact-target field in this form.

### Target and scope pickers

```text
Select deletion targets             What to delete
-------------------------------     --------------------------------
> [ ] <session ID>                   Current selection: Container only
  [x] <other session ID>
  [ ] <unmatched container name>     > * Container only
                                      Container and saved data/history
<focused target's folder detail>
```

Target choices include inventory sessions and unmatched managed containers.
They toggle immediately in the local selection; Back returns to the form
(no Done row). Scope selection marks/focuses the existing scope. Empty target
inventory yields an actionless picker.

### Preview, confirmation phases, and outcomes

Preview is a **Deletion preview** read-only screen using the same scope and
filters as execution. It lists Would delete container/session/incomplete
creation directory, retained state, and any applicable no-resources result.

Delete runs service preflight and asks only the relevant confirmations:

1. **Container phase:** exact container name(s), then Remove container(s)?
2. **Saved-data phase:** only for whole-session scope, and only after any
   requested container phase succeeds. Shows Container removed / count,
   or No container for a missing single-session container, then Delete saved
   data and history?
3. **Incomplete creation variant:** names the incomplete directory/directories,
   warns that all their copied files are removed and images retained, then
   Delete incomplete creation files? when no saved session is involved.

Each uses the shared No-first confirmation. Declining saved-data deletion
can retain history after the container has already been removed. Errors may
also carry partial results. The foreground output lists actual Deleted,
retained, Cancelled, or No resources deleted outcomes before acknowledgement.
The form exits when something was deleted successfully; a cancellation with
no deletion leaves it open. Container-only scope does not remove incomplete
creation files and explains their retention.

## 12. Read-only views and help

**Owners:** `frontend.go`, `status.go`, `config_show_render.go`,
`frontend_operations.go`.

All views below use Layout C with no selectable actions and support Back/Enter
and scrolling. These are graphical wrappers around existing output renderers,
not independent editors.

| Title | Body and conditional states |
|---|---|
| Status | Session/container summary; pending transfer mode/phase/endpoints; error/default error; session ID, harness, container name/ID, image, active-command count, lifetime; missing-image/runtime guidance; Changes classification; desired-config error; scoped change reasons and exact Recreate guidance where applicable |
| Status · all sessions | Table: Folder, Name, Harness, Container, Change; per-session change/error/missing-runtime/image guidance; broken-default warnings; unmatched-container warnings |
| Combined configuration | Session configuration path/ID; ordered layers; sorted effective setting rows and scalar/per-entry provenance; artifacts; harness name/origin |
| Network facts | Pretty JSON: name, mode, primary, host, gateway, networks/endpoints |
| Network environment | All four network variables as shell export lines |
| `<network variable name>` | Just that variable's value; four possible titles listed under Networks |
| Transfer preview | Pretty JSON transfer result; endpoints, mode, destination identity, source references and result flags as provided by the service |
| Deletion preview | Would-delete targets, retained data/incomplete directories, and no-resources output |
| Commands | Root command usage/help text |

Status Changes classifications are: **No changes**, **Runtime changes**,
**Recreate needed**, **Rebuild + recreate needed**, and **Cannot check**.
Errors, pending transfers, and invalid desired config produce Cannot check.
The detailed status uses automatic-vs-until-stop lifetime wording. Its
container summary can include `!` for errors and `*` for pending transfer;
these are distinct from the browser's default `*` after a session name.

Schematic examples:

```text
Status                          Combined configuration
----------------------------    -----------------------------------
/work/project / Main stopped    session configuration: <session-id>
Session: <session-id>              layer: default
Harness: pi                       layer: base (<directory>)
Container: <name> (<id>)           <setting>  <value>  <source>
Image: <image-id>                <artifact>: <path>
Active commands: 0              Harness: pi (<origin>)
Lifetime: automatic (...)
Changes: Runtime changes
  - [runtime] <reason>
To apply changes: dbx recreate <session-id>
```

### Help and shell integration

```text
Help and shell integration
---------------------------------------
> Command reference
  Completion · bash
  Completion · zsh
  Completion · fish
  Completion · powershell
```

Command reference has the current version as its action detail and opens
Commands. There is no separate selectable Version row. Completion actions
print the script in the normal terminal with foreground acknowledgement;
they do not install it and are not graphical script viewers.

## 13. Migration gate

**Owner:** `migration.go`.

```text
Migration required
----------------------------------------------------
Migration  <required migration name>
Home       <selected home>
Changes    <description>
Warning    <consequences>

> Exit
  Migrate
```

A standalone blocking screen on stderr, before a state-backed command or
browser can proceed. Exit is the initial selection and is also the Back
behavior. Migrate is destructive-styled and is the explicit approval: there
is no second confirmation or migration picker. It leaves the alternate screen
to apply the migration. Success continues the original requested action;
decline, cancellation, and failure keep it blocked. Noninteractive and JSON
calls return a migration-required error instead of this screen.

## 14. Text-entry and confirmation catalog

This consolidates the shared widgets' concrete uses so small prompts do not
get lost among larger menus. All text rows use **Enter a value**, with the
contextual prompt below. Source prompts mentioning `:back` are displayed with
`Esc` in the graphical UI.

| Caller | Prompt / initial state |
|---|---|
| Browser: edit config by path | `Config name or directory:`; empty |
| Browser: open folder | `Workspace folder:`; empty |
| Create session: name | `Session name (:back cancels):`; current name or empty; folder/current-name context |
| Create session: folder | `Workspace folder:`; current folder |
| Session: workspace | `New workspace (a matching old-folder default will be cleared):`; current workspace |
| Existing-config picker: directory | `Config directory (:back cancels):`; empty for add, current reference path for replacement |
| Create config: destination | `Config name or directory (:back returns):`; accepted name/path or empty |
| Scalar config setting | `New value (:back cancels):`; literal local value or empty |
| List setting add/edit | `New argument/option/mount/environment variable/port forward/extension (:back cancels):`; current entry for edit; environment input masked |
| Invocation argument add/edit | `Exact argument (empty is allowed):`; exact current value for edit |
| Invocation arguments JSON | `JSON array of arguments:`; current array or `[]` |
| Exec | `Executable:`; draft value, initially empty |
| SSH | `Destination:`; draft value, initially empty |
| Logs | `Tail:`; initially `100` |
| Network change | `Existing Docker network:`; empty |
| Transfer destination | `Destination folder (empty keeps source folder):`; draft value; unavailable while pinned |
| Transfer name | `Destination local name (empty preserves name):`; draft value; unavailable while pinned |
| Rename | `New session name:`; draft value, initially empty |
| Bulk deletion age | `Inactive longer than (empty disables):`; draft value or supplied CLI duration |

All production confirmation variants:

| Caller | Question / warning |
|---|---|
| Recreate, single/all | Apply current config; runtime may restart; replacement loses container-local changes |
| Transfer | Copy/move exact source → destination; workspace/config files not copied |
| Abort pending transfer | Discard uncommitted destination attempt, retain source |
| Delete container(s) | Exact names; Remove container(s)? |
| Delete saved data/history | Saved targets and previous container outcome; Delete saved data and history? |
| Delete incomplete creation files | Exact directories; copied files removed, images retained |
| Delete named config | Exact config name and directory; all files removed; service checks saved-session references first |

Start, Stop, default selection/clearing, name/workspace edits, individual config
edits, and config/session creation have no extra Yes/No confirmation. Their
explicit actions/forms are the submission boundary. Migration approval uses
its own Exit/Migrate screen rather than the common confirmation widget.

## 15. Navigation routes and keystrokes

### Reading the routes

In the tables below, **Select `Label`** means Up/Down (or `j`/`k`) until that
row is focused, then Enter. Where useful, `/` + a distinctive part of its label
+ Enter finishes filtering; a **second Enter** activates the highlighted match.
Filtering includes descriptions/values, so a label fragment is not always
unique. Prefer the explicit row name over memorized row numbers: conditional
rows and filtering change positions.

**Back** means Esc or `q` outside text entry. An applied filter consumes the
first Back to clear it. The root browser keeps object focus; global actions
have their separate Browser actions menu. In a nested screen with navigation, Left
also goes back; in text input Left/Right edit the caret instead. Cancel labels
on creation forms and value pickers use the same Esc navigation, not a visible
Cancel action row. Ctrl-C is not a safe synonym for Back: it cancels the
command while the graphical UI owns the terminal.

Every child picker/view returns to the screen that opened it, even when that
same component has several callers. Direct-command entry has no outer browser
to return to: closing its top-level workflow finishes that command.

### Top-level navigation map

```text
dbx --------------------------> Sessions browser
                                  | Tab
                                  v
dbx config -------------------> Configs browser

Sessions browser
  Enter on folder ------------> Folder menu
  Enter on session -----------> Session menu
  Right ----------------------> Highlighted object's menu
  b --------------------------> Browser actions menu
  n --------------------------> Create session
  a --------------------------> All-session operations
  r --------------------------> Refreshed Sessions browser

Configs browser
  Enter on config ------------> Config settings editor
  Right ----------------------> Highlighted object's editor
  b --------------------------> Browser actions menu
  n --------------------------> Create config
  r --------------------------> Refreshed Configs browser

Nested workflow -- Back ------> Its caller
Read-only view --- Enter/Back -> Its caller
Text input ------ Enter ------> Accept, then its caller
Text input ------ Esc --------> Discard unfinished input, its caller
Confirmation ---- No/Back ----> Decline; caller decides next screen
```

### Browser, folder, and session routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Either browser | Tab | Other browser; clears prior tab's filter/cursor state |
| Browser objects | Right or Enter | Highlighted object's workflow; target retained, no operation executed |
| Either browser | `b` | Browser actions menu; choose a global command, or Back to the browser |
| Browser objects | Select folder/session/config object | Folder menu / Session menu / Config editor; Back returns to browser |
| Sessions browser | `n`, or Select Create session | Create session; successful creation enters new Session menu; cancel returns to browser |
| Configs browser | `n`, or Select Create config | Create config; create/cancel returns to Configs browser |
| Sessions browser | `a`, or Select All-session operations | All-session operations; Back returns to browser |
| Either browser | `r`, or Select Refresh | Same browser rebuilt synchronously; preserve stable object identity when possible |
| Browser actions (Sessions) | Select Open folder by path → enter path → Enter | Folder menu; Esc in input stays in browser |
| Browser actions (Sessions) | Select Sort sessions → Select Name/Last active | Same browser with chosen within-folder sort; Back in picker preserves prior sort |
| Browser actions (Configs) | Select Edit a directory by path → enter name/path → Enter | Config editor; Esc in input stays in browser |
| Browser actions (either browser) | Select Help and shell integration | Help menu; Back returns to browser |
| Folder menu | Select Create session here | Create session prefilled for that folder; cancel returns to folder menu |
| Folder menu | Select Clear folder default | Same folder menu, default updated; no confirmation |
| Session menu | Select Continue / Open / Shell / Start; or `c` / `o` / `s` for those access actions | Foreground terminal; after result acknowledgement, same Session menu |
| Session menu | Select Open with options / Exec / SSH / Logs / Networks / Stop / Recreate; or `e` / `l` / `r` for those forms | Corresponding form; Back returns to Session menu |
| Session menu | Select Status or press `i` | Status view; Enter/Back returns to Session menu |
| Session menu | Select Selected configs | Manage configs; Back returns to Session menu |
| Session menu | Select Make/Clear folder default | Same Session menu, label/marker updated; no picker or confirmation |
| Session menu | Select Copy or move | Transfer form; copy/Back returns here; successful move leaves this old Session menu |
| Session menu | Select Rename session | Rename form; Back returns here; success leaves Session menu and refocuses renamed ID in browser |
| Session menu | Select Change workspace → edit → Enter | Same Session menu with saved-reference notice; Esc leaves reference unchanged |
| Session menu | Select Delete | Deletion form; Back/container-only deletion returns here; removed saved session/incomplete directory leaves this Session menu |

A quick current-folder path is: **`dbx` → Enter → Enter**. If initial focus is
a session in the current folder, the first Enter opens its menu and the
second activates Continue. Otherwise first choose the desired session; the
shortcut does not assume or change a folder default.

### Creation and config-chain routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Create session | Select Set/Change session name → edit → Enter | Same creation form, draft name updated; Esc preserves prior name |
| Create session | Select Change folder → edit → Enter | Same creation form, draft folder updated |
| Create session | Select Make folder default | Same creation form, toggle flipped; no saved default change yet |
| Create session / Manage configs | Select Add existing config | Existing-config picker; selecting a row appends it and returns |
| Create session / Manage configs | Select Create config | Config creation form; successful create appends config and returns; Cancel leaves chain alone |
| Create session / Manage configs | Select Replace config → Select current chain entry | Existing-config picker with that reference marked; accepting replaces it and returns |
| Create session / Manage configs | Select Remove config → Select chain entry | Same parent with removal applied to draft/saved chain; sole saved config is blocked before opening picker |
| Create session / Manage configs | Select Reorder configs → Select chain entry → Select Position N | Same parent with reordered draft/saved chain |
| Existing-config picker | Select candidate | Accept selection, return to chain caller; failed validation stays in picker |
| Existing-config picker | Select Enter a directory path → edit → Enter | Accept typed selection and return; Esc stays in picker |
| Empty existing-config picker | Select Create and add config | Create config; success accepts it and returns to chain caller; cancel returns to empty picker |
| Manage configs | Select Show combined configuration | Combined configuration view; Enter/Back returns to Manage configs |
| Create session | Select Create session, when unblocked | Foreground creation → Enter acknowledgement → new saved Session menu; failure returns to populated creation form |
| Create session | Back/Cancel | Caller; discard session draft, not separately created configs |
| Manage configs | Back | Caller; completed chain edits remain saved |
| Create config | Select Name/location → edit → Enter | Same form, accepted destination updated; Esc preserves destination |
| Create config | Select Harness → Select definition/Leave unset | Same form, draft harness updated; Back preserves accepted harness |
| Create config | Select Optional files | Optional-files menu; Continue accepts draft, Back abandons child changes |
| Optional files | Select setup.sh / before-open.sh / docker/Dockerfile | Same menu, toggle flipped |
| Optional files | Select unchecked Harness config files | Harness-file-target picker; selecting definition returns with files checked; Back leaves them unchecked |
| Optional files | Select checked Harness config files | Same menu, unchecked; no target picker |
| Optional files | Select Continue | Creation caller: update draft; config-editor caller: add missing files now; return to caller |
| Create config | Select Create config, when unblocked | Publication and return to caller on success; no nested config editor is opened automatically |
| Create config | Back/Cancel | Caller; no config publication |

### Config-editor routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Config dashboard | Select Harness / Network / Base image | Scalar submenu; Back returns to dashboard |
| Config dashboard | Select any list setting | Its list submenu; Back returns to dashboard |
| Scalar submenu | Select Edit value | Text entry, except Harness opens New value choice picker when definitions exist |
| Harness New value picker | Select harness | Save value and return to Config dashboard |
| Harness New value picker | Select Enter a value or host expression | Text entry; accepted save returns to Config dashboard; Esc returns without saving |
| Scalar text entry | Edit → Enter | Validate/save; success returns to Config dashboard; rejected value retained for correction |
| Scalar submenu | Select Remove this setting | Save removal and return to Config dashboard |
| List submenu | Select Add `<noun>` → enter value → Enter | Save entry and return to same list submenu |
| List submenu | Select Edit `<noun>` → Select entry → edit → Enter | Save replacement and return to same list submenu |
| List submenu | Select Remove `<noun>` → Select entry | Save removal and return to same list submenu; no confirmation |
| List submenu | Select Remove this setting | Save key removal and return to same list submenu, now showing Add only |
| Config dashboard | Select Add optional files | Optional-files menu; Continue adds files and returns to dashboard |
| Named config dashboard | Select Delete config | Safety checks → confirmation; No/Back stays in dashboard; Yes deletes and closes editor |
| Config dashboard | Back/Exit | Caller / finish standalone command; completed edits remain saved |

Scalar cancellation returns to the dashboard because the scalar submenu is a
one-shot choice, not a persistent loop. Cancellation of a list-entry picker
or input returns to the list submenu. After a concurrent-edit error the editor
reloads instead of retaining a stale write attempt.

### Operation-form routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Open with options | Select Continue previous conversation | Same form, toggle flipped |
| Open with options | Select Harness arguments / Trailing arguments | Harness arguments / Arguments appended last editor; Back keeps draft edits |
| Exec | Select Executable → edit → Enter | Same form; Run command becomes available after accepted nonempty value |
| Exec | Select Arguments | Command arguments editor; Back keeps draft edits |
| Any argument editor | Select Argument N | Argument choice submenu |
| Argument submenu | Select Edit value → edit → Enter | Same argument editor, value replaced; Esc abandons unfinished value |
| Argument submenu | Select Remove | Same argument editor with entry removed |
| Any argument editor | Select Add argument → type → Enter | Same editor, exact value appended |
| Any argument editor | Select Set arguments from JSON → edit → Enter | Same editor, whole array replaced; invalid JSON retained for correction |
| SSH sharing | Select Destination → edit → Enter | Same form, accepted destination updated |
| SSH sharing | Select Use host SSH master | Same form, toggle flipped |
| Docker logs | Select Tail → edit → Enter / Select Follow until interrupted | Same form, draft value/toggle updated |
| Stop container | Select Force: allow interruption of active commands | Same form, toggle flipped |
| Rename session | Select New session name → edit → Enter | Same form, accepted draft name updated |
| Recreate | Select either force/rebuild toggle | Same form, toggle flipped |
| Recreate | Select Inspect pending changes | Status or Status · all sessions; Enter/Back returns to Recreate |
| Recreate | Select Recreate or press `r` | Confirmation; No/Back stays in form; Yes → foreground → Enter acknowledgement → parent on success |
| Open/Exec/SSH/logs/Stop/Rename form | Select Open/Run command/Connect/Read logs/Stop container/Rename session | Foreground → Enter acknowledgement → parent on success; failure retains form |
| Networks | Select Inspect networks | Network facts view; Enter/Back returns to Networks |
| Networks | Select Environment exports | Network environment picker; Back returns to Networks |
| Network environment picker | Select All exports / variable | Export/value view; Enter/Back returns to export picker |
| Networks | Select Connect/Disconnect network → type name → Enter | Foreground network change → Enter acknowledgement; success closes Networks, failure retains it |
| Any operation form | Back | Caller; discard invocation draft, without undoing any operation already completed |

An interrupted form submission can remain in its form if interruption is
returned as an error. An operation that normalizes interruption to success,
such as expected SSH disconnection, follows the success path. Foreground
Ctrl-C does not automatically navigate Back.

### Transfer, bulk operations, and deletion routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Copy or move | Select destination/name → edit → Enter | Same form with draft updated; pinned pending rows instead show blocked notice |
| Copy or move | Select Move toggle | Same form with mode changed; pinned row does not toggle |
| Copy or move | Select Preview | Transfer preview; Enter/Back returns to form |
| Copy or move | Select Transfer | Preflight → confirmation; No/Back stays; Yes → foreground → acknowledgement → caller, or leave old session menu after move |
| Pending prepare form | Select Abort pending transfer | Confirmation; No/Back stays; Yes → foreground → acknowledgement → source/caller |
| All-session operations | Select Status for all sessions | All-status view; Enter/Back returns |
| All-session operations | Select Recreate all | Recreate form scoped to all; Back/success returns |
| All-session operations | Select Bulk deletion | Delete sessions form; Back/success returns |
| Bulk deletion | Select Choose exact targets | Target picker; Select row flips check; Back accepts local selection and returns |
| Bulk deletion | Select filter toggle / age field | Same form with selection draft updated |
| Any deletion form | Select Delete scope | What to delete picker; Select scope returns to form; Back preserves scope |
| Any deletion form | Select Force | Same form, Off/On flipped |
| Any deletion form | Select Preview deletion | Deletion preview; Enter/Back returns |
| Any deletion form | Select Delete | Preflight → container/data confirmations as applicable → foreground results → Enter acknowledgement; deleted-count success closes form |
| Deletion confirmation | No/Back | Decline this phase; earlier completed container removal is not rolled back |
| Session-menu deletion of saved data/incomplete files | Complete deletion | Close deletion form and the now-invalid Session menu, returning to caller/browser |

### Help, migration, and terminal handoff routes

| From | Keys / action | To / return behavior |
|---|---|---|
| Help menu | Select Command reference | Commands view; Enter/Back returns to Help |
| Help menu | Select Completion · `<shell>` | Normal-terminal script output → Enter acknowledgement → Help |
| Migration gate | Enter on initial Exit, or Back | Original command ends with migration-required error; no browser follows |
| Migration gate | Down once → Enter on Migrate | Apply migration; success continues original command/browser; failure/cancellation ends blocked |
| Any confirmation | Enter on default No, or Esc | Decline; use caller-specific route above |
| Any confirmation | Right (or Down once) → Enter | Approve; use caller-specific route above |
| Foreground harness/shell/command | Exit child normally, or Ctrl-C where appropriate | Existing cleanup → normal-terminal result/acknowledgement; Enter resumes workflow |
| Foreground follow-logs/SSH | Ctrl-C | End foreground operation; result/acknowledgement then resume according to success/error path |
| Working graphical screen | Ctrl-C | Cancel command; does not submit another menu action |
| Any graphical screen | SIGTERM/SIGHUP | Cancel command; do not return to a still-running browser |
| Root browser, no active filter, object focus | Esc / `q` | Close browser and return to invoking shell |

No row-number or mouse navigation is required by these routes. The shared
single-line input supports editing/paste through its text widget; menu actions
still require explicit Enter. The normal-terminal acknowledgement is a line
read: press Enter there, not Esc expecting graphical Back behavior.

## 16. Coverage and source map

This inventory was checked against production `Screen`, `form`, `Select`,
`SelectCurrent`, `View`, `Text`, and `Confirm` call sites, plus conditional
Hidden/Blocked/Checked/Selected actions. The boolean setting branch is noted
as unreachable rather than counted as a current menu. Test-only fixture
screens are not included.

| Area | Source / representative existing tests |
|---|---|
| Frames, focus, responsive layout, keyboard states | `internal/cliui/tui.go`, `ui.go`, `terminal.go`; `tui_test.go`, `browser_test.go`, `text_test.go`, `navigation_test.go`, `navigation_refresh_test.go` |
| Browsers, folder/session menus, help, foreground handoff | `internal/cli/frontend.go`; `frontend_browser_test.go`, `frontend_terminal_test.go`, `frontend_operations_test.go`, `session_ux_test.go`, `defaults_test.go` |
| Forms, networks, arguments, transfer, deletion | `internal/cli/frontend_operations.go`, `rename.go`, `delete.go`; `frontend_test.go`, `frontend_operations_test.go`, `application_test.go`, `delete_incomplete_test.go` |
| Session creation | `internal/cli/session_create.go`; `create_default_test.go`, `frontend_creation_terminal_test.go`, `named_workflows_test.go` |
| Config chains and suggestions | `internal/cli/source_menu.go`, `source_picker.go`; `ui_workflows_test.go`, `ui_saved_workflows_test.go`, `source_candidates_test.go`, `source_picker_test.go` |
| Config creation and optional files | `internal/cli/config_create_flow.go`, `setup_menu.go`; `config_create_flow_test.go`, `harness_defaults_test.go` |
| Config dashboard and editors | `internal/cli/config_menu.go`, `config_delete.go`, `internal/resource/config_edit.go`; `config_menu_presentation_test.go`, `config_menu_sources_test.go`, `config_input_test.go` |
| Read-only content | `internal/cli/status.go`, `config_show_render.go`, `containers.go`, `internal/app/network.go` |
| Migration | `internal/cli/migration.go`; `migration_test.go`, `migration_terminal_test.go` |
| Entry routing | `internal/cli/root.go`, `resources.go`, `source_menu.go`, `session_create.go`, `delete.go` |

Not separate dbx graphical screens: harness-owned UI, shell/SSH authentication
prompts, Docker build/log streams, normal CLI output, shell completion menus,
plain numbered-prompt fallbacks, and the normal-terminal Enter acknowledgement.
Their handoff boundaries are recorded above, but their external interfaces are
not owned by this menu system.

There is no current separate default-session picker, container-actions
submenu, global Save/Discard menu, network-discovery picker, or standalone
harness-definition editor. This describes current call paths, not older
planning documents. No live Docker or manual terminal walkthrough was run
for this scouting document; sketches and states are source/test-derived.
