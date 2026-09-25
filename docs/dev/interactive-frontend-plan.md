# Interactive frontend

Status: approved interaction model; implemented on `feature/bubbletea-frontend`. See [progress](progress.md) for validation and outstanding acceptance checks.

## Browse an object, then act on it

Bare `devbox-neo` opens Sessions. Bare `devbox-neo config` opens Configs. Both require terminal input/output; otherwise they show help without opening state. Direct commands, flags, JSON, and completions remain available.

The header uses **Devbox Neo**, with Sessions/Configs tabs and the Tab hint beside them. No alias branding, tagline, implementation commentary, or repeated shortcut legend.

- The left pane contains only objects: folders and their sessions, or named configs.
- Highlighting previews structured details on the right. It does not open a harness or change state.
- Enter opens the selected object's menu/editor on the right. The object list stays visible on wide terminals, with its selected object marked but no longer owning keyboard input.
- Escape returns to browsing, retaining the selected object's identity. Refreshes must not restore by row number when inventory order changes.
- Application actions are separate from the object list. Right moves focus to them; Left returns to the object list. Their shortcuts appear beside their menu options only.
- Tab switches collections while browsing, not while filling a form or using a nested editor.
- Narrow terminals show the active pane. Filtering, scrolling, and all operations remain available by keyboard.

Details use aligned muted labels, emphasized values, colored status, ordered config entries, and distinct warnings. Full container identity remains available as a secondary detail. Do not repeat Home/Version in every object's details. Use consistent terminal backgrounds; only a focused row needs a selection background.

## Sessions and folder defaults

Enter on a session opens its menu, with Open focused:

- Open, Continue, Shell, Status, and Edit selected configs.
- Container actions: Start, Stop, Recreate, Logs, Networks.
- Copy or move.
- Exec, SSH and launch arguments.
- **Make folder default**, replaced by **Clear folder default** when this session is the default.
- Delete.

Setting a default already has an exact session and folder target. It needs no second picker. Clear belongs at the same menu level. After either operation, refresh the marker and details immediately; preserve the current keyboard location. Selecting a session by itself never changes defaults.

Folder entries offer Create session here and Clear folder default. Folder-level clearing also handles a saved default whose session is no longer selectable. Open folder by path reaches folders absent from the inventory. The browser has no separate routine Set-default picker.

The explicit `edit <folder>` command retains its folder/config editor, including the aligned Set/Clear picker. Its flags and scripting contract are unchanged.

Operational preconditions remain service-owned. Preserve missing-container recovery only where the underlying command supports it, ownership checks, explicit force, active-command protections, exact transfer endpoints, and separate container/history deletion decisions. A menu error must not silently change an operation's meaning.

## Configs

Enter opens the selected config's settings editor directly, not an intermediate Edit/Delete chooser. Show field values and their resolver-provided origins. Optional-file controls and eligible named-config deletion belong in that same editor.

Config browsing works without Docker. Invalid configs remain visible with diagnostics. Path-based configs have no global registry and are reached through Edit a directory by path. Existing named-directory deletion restrictions and usage checks remain unchanged.

## First creation

An empty session browser shows **No sessions yet**, with Create session focused. No empty tables, zero counters, or onboarding prose.

Create session opens one draft:

- Folder is visibly prefilled from the current directory, or from Create session here. It remains editable in the browser workflow.
- The session name starts unset.
- Configs start empty. Add existing config and Create config remain available.
- Nested config creation uses the shared Name/location, Harness, Optional files overview. There is no tutorial paragraph in the form.
- Successful config creation adds the new reference and returns directly to the parent draft. Cancellation or failure preserves that draft. A created config remains independently saved.
- Create session stays visible and reports missing inputs when selected. It streams the real build through terminal handoff.
- Failed builds return to the populated draft. Partial outcomes retain the service's recovery instructions; do not adopt, delete, or blindly overwrite partial resources. If creation committed but stopping failed, open that saved session's menu for recovery.
- Success opens the new session's menu and selects it in the browser on return. It remains stopped, without a default or attached harness.

Open from a session menu does not require a folder default. The harness owns its authentication/setup after launch.

Direct `create <folder>` keeps its explicit target, streamed build, receipt, and exit behavior. It shares input/config selection with the browser, not its navigation lifecycle.

## Bulk operations and forms

Application actions include creation, Refresh, Help/completion scripts, and all-session operations. Bulk status, recreate-all, and deletion remain distinct from single-session actions. Sort changes sessions within their folder groups; direct `list --sort` keeps its whole-table semantics.

Forms collect only the missing arguments for an operation. Exact argv editing includes empty/control-character arguments without implicit shell parsing. Copy/move previews endpoints before confirmation; pending transfers pin their recorded endpoints/mode. Deletion preserves the existing two stages and explicit force rules.

## Terminal ownership

One command-scoped Bubble Tea program owns menu input and rendering. Synchronous workflow functions own navigation and call existing application/resource/store services. Object collections, action lists, and structured detail fields are explicit presentation data, not a combined generic command list. Screen snapshots passed to the UI contain no domain handlers.

Nested functions share the runner. Navigation snapshots keep the parent collection visible; they do not introduce a router, domain event bus, or second inventory owner. Inventory is refreshed on entry/actions or explicit Refresh, not in a background loop.

`Pause` releases both renderer and input reader before foreground programs, streaming output, confirmations' audit output, and acknowledgements. Restore terminal flags before reviewing an operation's result, then return to the same menu. Foreground Ctrl-C cancels the operation rather than the browser; menu Ctrl-C and SIGTERM cancel the command. Confirmation starts on No.

Existing services retain persistence, validation, locking, ownership, lifecycle, transfer recovery, and deletion safety. Do not recursively execute Cobra or add compatibility paths.

## Validation

Cover object/action separation, stable selection, pane focus, filtering, empty states, narrow layouts, structured rendering, direct default changes, nested config creation, retained build drafts, no implicit launch/default, and terminal handoff/restoration. Keep direct-command and service regression coverage.

Tests and rendered-screen inspection are not live Docker, harness authentication, SSH, or manual host-terminal acceptance. Record those gates honestly in progress notes.
