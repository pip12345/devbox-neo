# Proposal: folder-local sessions with explicit configs

Status: implemented; live acceptance pending. This replaces the previous generic-config alternative and the profile/project selection model. Implementation checkpoints, automated validation, and unpassed acceptance gates are tracked in [progress.md](progress.md). The separately approved `devbox-migrate` adaptation targets this model; ordinary runtime loading gains no migration or compatibility path.

## The whole model

A **session** is a saved development environment for one workspace folder. It has:

- A folder-local name supplied through `--name` or the interactive creation flow.
- A full session/container name for exact targeting from any folder.
- An ordered list of config directories.
- Durable session state and a replaceable container.

A **config directory** supplies settings, Dockerfiles, scripts, and harness files. It is an ordinary directory that multiple sessions can share.

A folder can have several sessions and one explicitly selected default session. The default only chooses a session; it does not supply configuration.

There are no profiles, special project config types, automatically discovered config layers, or names derived from config choices.

Remove the global configuration layer and its editing commands. There is no global environment list, default harness, default profile, or project-discovery setting. Environment settings come from built-in defaults followed by the session's explicit config sources. Put shared harness selection and environment settings in an ordinary reusable config such as `base`.

## First use and command entry points

Commands below use the proposal's `devbox` spelling; the development executable remains `devbox-neo`. `<home>` means the selected Devbox home: `--home` takes precedence over `DEVBOX_HOME`, then the binary's default. The rewrite's default remains `~/.devbox-neo`, with its existing rejection of the old `~/.devbox` home. Named configs live in `<home>/configs`, including when a custom home is selected; no config lookup bypasses that selection.

After installing the CLI and Docker, a first-time user runs these commands from their workspace:

```sh
devbox config create base
devbox create .
devbox edit .
devbox open .
```

`config create base` creates a reusable config and offers harness/artifact setup. Optional customization files can be skipped. `create .` asks for a session name and lets the user select the existing `base` config. In `edit .`, choose **Set folder default** and select that session. `open .` starts it and launches its harness. Later visits need only `open .`, optionally with `-c` to continue the previous harness conversation.

Each interactive workflow has one command entry point:

| Command | Responsibility |
|---|---|
| `config create <reference>` | Create a config directory and offer its initial setup |
| `config edit <reference>` | Edit an existing config directory and add missing optional files |
| `config list [--json]` | List named configs under the selected home's `configs/`, including invalid or incomplete entries; path-based configs are not registered here |
| `config delete <name>` | Remove an unreferenced named config under the selected home's `configs/` after confirmation; refuse desired or committed session users and list every known user |
| `create <folder>` | Name a session and select existing config sources |
| `edit <folder|session>` | Manage a session's source chain, inspect combined configuration, and select or clear its folder default |

Keep directory creation/editing out of session creation and source-chain menus. Those menus may print the exact command to run next, but must not launch another command's menu. Reuse the existing underlying configuration mechanisms without duplicating their interactive entry points. Top-level `edit` replaces the bare `config <folder|session>` form; do not retain the old `config sources` command as an alias or add a root-level `sources` command.

## Shared menu presentation and controls

Follow the rewrite's existing menu and settings-display components in `internal/cli/menu.go` and `internal/cli/config_display.go`, with the terminal conventions used by `internal/cli/list.go` and `internal/cli/terminal.go`. The examples below are plain-text layouts; terminal styling is part of the contract, not decoration left to each command.

- Titles say what the user is doing or selecting. Label context explicitly, such as `Folder:` and `Session:`; do not use a command name as a substitute for explaining a screen.
- Use bold headings, aligned indented `[1]` choices, and the existing `Choose a number >` prompt. Use dim context labels, paths, and secondary instructions.
- Selection menus show `Current selection:` above the choices and emphasize its value. Mark the selected choice with a bold name and a green `(selected)` marker. Multi-select menus use the same emphasis and green checkmarks, and summarize the selected items above the choices. Do not add selection summaries to ordinary action menus or imply a selection that has not been made.
- Preserve the session list's distinction between running and inactive sessions: stopped/missing entries are dimmed, but a selection marker must remain prominent. Status and selection are separate facts. Errors have explicit labels and are never indicated by color alone.
- Keep the settings dashboard's `Setting / Value / Source` columns. Show scalar values and shell commands inline, other lists underneath their setting with a source beside each entry, and empty values as `None`. Wrap long values and paths rather than truncating them; use the existing terminal-width limit.
- Apply styling through shared terminal helpers, not command-specific ANSI fragments. Align plain text before styling so escape sequences do not shift columns. Respect output-terminal detection, `NO_COLOR`, `TERM=dumb`, and redirected output. Plain text retains `(selected)`, checkmarks, and status/error labels.
- `[0] Back` returns to the previous step or menu. Use `[0] Cancel` when abandoning a selection or creation flow, and `[0] Done` when leaving an editor entered directly. `q` follows the displayed `[0]` action. Do not show Back when there is no previous screen.
- Keep canonical line input: type a number and press Enter. Text entry uses `:back` to cancel the unfinished input, for example `New mount (:back cancels): `. Do not add Escape handling, raw-terminal controls, or a new keyboard-input system. EOF abandons incomplete input while retaining completed edits. One shared menu renderer redraws short screens in the terminal's temporary alternate screen, without changing input mode; oversized or unpredictable-width menus print normally. Restore the shell screen before printing final results or errors. Redirected output stays plain.
- Completed setting and saved-source-chain edits save immediately. Back only navigates; there are no Save/Discard screens or extra approvals. Creation choices remain pending until the creation action; Back between creation steps preserves those choices without publishing files or a session.

## Create sessions

Creating a session requires an explicit `devbox create <folder>` command. The folder argument is mandatory: use `devbox create .` for the current folder. Do not infer it from an omitted argument. `open`, `edit`, and configuration inspection never implicitly create sessions.

With all required inputs supplied, creation runs directly:

```sh
devbox create . --name myenv1 --config blah
devbox create . --name myenv2 --config ./myconfig
devbox create . --name myenv3 --config ~/coolconfig
devbox create . --name myenv4 --config /abs/path/to/config/
```

Config references resolve as follows:

| Reference | Meaning |
|---|---|
| `blah` | `<home>/configs/blah` |
| `./myconfig` | `myconfig` relative to the invoking working directory |
| `configs/local` | `configs/local` relative to the invoking working directory |
| `../shared` | A relative filesystem path |
| `~/coolconfig` | A home-relative filesystem path |
| `/abs/path/to/config/` | An absolute filesystem path |
| `.` or `..` | The current or parent directory |

A reference containing `/` is a path. Bare config names cannot contain `/`; they identify directories under `<home>/configs/`. The special directory references `.` and `..` are paths too. There is no local-then-global search or fallback.

`<home>/configs/` is a convenience location, not a registry or a different kind of config. Under the rewrite's default home, `base` resolves to `~/.devbox-neo/configs/base`. Under `--home /srv/devbox`, it resolves to `/srv/devbox/configs/base`. Creation, editing, source selection, and config pickers all use this same rule. Use `./blah` to select a local directory named `blah` instead of the shorthand location. Explicit filesystem references such as `~/coolconfig` remain ordinary paths and do not depend on the selected Devbox home.

The workspace and configs are independent inputs. Relative config paths initially resolve against the invoking working directory, not against the workspace argument. Save each reference in its selected order using one of two forms:

- **Workspace-relative:** a relative path argument is resolved first, then saved relative to the session's canonical workspace. Resolve that saved reference against the session's workspace on later operations.
- **Fixed:** an absolute path, home-relative path, or bare config name is saved as an absolute path. It does not follow the workspace when the session is copied or moved.

For example, from `/work`, `devbox create api --name main --config ./api/devconfig` saves the workspace-relative reference `devconfig`. Using `--config /work/api/devconfig` instead saves that fixed absolute reference. Relative references may contain `..`; they retain their relationship to the workspace even when they point outside it.

Creation saves references, not copied configs or a merged snapshot. The reference form is explicit saved state; do not infer portability later from whether an absolute path lies inside the workspace.

Multiple configs use repeated flags:

```sh
devbox create . --name myenv1 \
  --config base \
  --config configs/project \
  --config ~/personal-config
```

The same configs can create multiple independently named sessions in the same folder. Changing a session's sources does not change its name or identity. Both configs in `--config 1 --config 2` participate, in that order; the second does not replace the first wholesale.

Creation requires at least one config source and a valid combined configuration, including a selected harness. Reject duplicate references to the same canonical config directory, including alternate spellings or symlink aliases: one source must not contribute settings or run its scripts twice.

### Interactive creation

In a terminal, `devbox create .` prompts only for the missing session inputs: the folder-local name and config source selection. Leave the name prompt blank; do not prefill or suggest `main` or another name. Require the user to enter a name. Partially specified commands prompt only for missing inputs:

```sh
devbox create . --name main                         # choose config sources
devbox create . --config base --config ./devconfig  # enter the local name
```

The source picker offers existing configs under `<home>/configs/` and an option to enter the path of an existing config directory. It does not create or edit directories, select their harness, or initialize their artifacts. If no reusable configs are listed, explain how to create one with `devbox config create base`; the user can still supply an existing directory path.

Name entry uses `Session name (:back cancels): ` with no prefilled text. The picker shows the ordered chain and supports adding, replacing, removing, and reordering sources before session creation. This example shows the summary after the user has entered `Main` and selected two sources; the name is not a default:

```text
Create session · Main

Folder: /work/api

Config sources, in order:
   1. base         fixed       ~/.devbox-neo/configs/base
   2. devconfig    relative    /work/api/devconfig

What would you like to do?

   [1]  Create session
   [2]  Add source
   [3]  Replace source
   [4]  Remove source
   [5]  Reorder sources

   [0]  Cancel

   Choose a number >
```

Plain numbered source rows show order; bracketed numbers identify selectable actions. With no sources, show only Add source and Cancel. Add source uses the existing-config picker:

```text
Select a config source

   [1]  base
   [2]  tools
   [3]  Enter a directory path

   [0]  Back

   Choose a number >
```

There is no preselected source when adding one. Replacement shows the current source above the choices and marks it when listed. Directory entry uses `Config directory (:back cancels): `. Reordering selects a source and then its new position through numbered choices, with Back to cancel an unfinished operation.

The interactive flow creates no session or container until the user selects Create session. It never creates or edits config directories. A fully specified command does not require a redundant confirmation. Without a terminal, missing required inputs produce an actionable error rather than prompting.

Successful creation leaves the container stopped and does not select a default. Print the local name, full session/container name, and resolved config sources in order, followed by concrete commands to select a default and open it. The user then runs `edit` to choose a default or opens the new session explicitly by name; do not launch the editor from session creation.

## Folder-local names and exact session targets

These are different sessions:

```sh
devbox create /work/apple --name dev --config base
devbox create /work/banana --name dev --config base
```

The local name, whether supplied through `--name` or the creation menu, is unique within its workspace folder. Allow 1–64 ASCII characters: uppercase and lowercase letters, digits, dashes, and underscores, beginning with a letter or digit. The validation rule is `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`. Preserve case and compare names case-sensitively; `Main` and `main` are different names. Do not truncate or silently normalize a local name. Invalid-name errors state the allowed characters and length. Local names cannot contain whitespace or path separators, or be `.` or `..`. Creating an already-used local name within the same folder fails; using that name in another folder is normal.

Workspace paths use canonical folder identity, so alternate spellings of the same folder do not create separate name scopes. Each saved session also has a full session name, identical to its managed container name. That full name is an exact target usable from any working directory, as in the existing Devbox interface. Names remain lookup keys, not proof of Docker ownership.

Keep the existing deterministic full-name shape, replacing the profile/project suffix with the local name:

```text
devbox-<folder>-<12-hex-hash>.<local-name>
```

- `<folder>` is the existing readable workspace-basename hint: lowercase it, replace runs outside `[a-z0-9_.-]` with `-`, trim leading/trailing `-_.`, cap it at 32 characters, and trim trailing `-_.` again. Use `workspace` when the result is empty. Only this hint is sanitized or shortened.
- `<12-hex-hash>` is the first 12 lowercase hex characters of SHA-256 over the canonical absolute workspace path, a NUL separator, and the exact case-sensitive local name. Config references and contents never enter this hash.
- `<local-name>` is the complete user-supplied name, preserving case. The longest full name is 117 ASCII characters, leaving ample room for existing session-directory and transfer-journal filenames.

For `/work/api`, `Main` produces `devbox-api-21d4e96b0656.Main`; `main` produces `devbox-api-239c7814b3f4.main`. Folder-plus-name lookup computes the full name directly; it does not scan other sessions or read config files. Validate that any loaded record matches the requested workspace and local name; an occupied or mismatching name is an error, never a reason to adopt state or silently choose another name.

Store the local name explicitly in session identity alongside the workspace and full name. Keep the existing durable session ID separate: recreation preserves it, and deleting then creating the same folder-local name produces a new ID. The existing Docker ownership-label and image-tag contracts remain independent of this naming scheme.

Lifecycle commands accept `<folder|session>`. Here, `<session>` means the **full container name**, not the folder-local `--name` value or a Docker container ID. In these examples, `<full-container-name>` is a placeholder for that exact name:

```sh
devbox open <full-container-name>    # exact session, from any folder
devbox open . --name myenv1          # local name within the supplied folder
devbox open .                       # supplied folder's default session
devbox shell <full-container-name>
devbox stop /work/apple --name dev
devbox recreate /work/banana --name dev
```

Preserve the existing distinction between full `devbox-...` session targets and folder paths. Do not interpret `devbox open myenv1` as a lookup of the folder-local name `myenv1`; use `devbox open . --name myenv1` for that. The config-reference shorthand rules do not redefine lifecycle target parsing.

## List sessions

```sh
devbox list .   # sessions belonging to this folder
devbox list     # all sessions, ordered by folder path in one table
```

The folder view shows local names, the default, container status, and config source order:

```text
/work/api

NAME         DEFAULT  CONTAINER  CONFIGS
main         *        stopped    base → ./devconfig
experiment            running    base → ./devconfig → ~/configs/experimental
```

The global view shows one row per saved session with a `FOLDER` path and full session/container name; there are no folder headings. `--sort folder` is the default and orders folder paths then names; `--sort name` orders full names, and `--sort last-active` orders all rows newest first. Listing includes saved sessions whose containers are missing, not just running Docker containers.

## Select a default

`edit` manages both a session's source chain and its folder's default selection. In `edit .`, choose **Set folder default**, then choose a numbered session from the picker. The folder overview shows the current default and offers **Clear folder default** when one is saved, even if that session is missing. Clearing needs no session selection or config resolution.

```sh
devbox edit . --name myenv1 --default       # select directly within this folder
devbox edit .                              # choose Set folder default, then a session
devbox edit . --clear-default              # clear this folder's default
devbox edit <full-container-name> --default # select for its recorded workspace
```

```text
Select a session to edit

Folder:  /work/api
Default: Main

   [1]  Main          default · stopped
   [2]  Experiment    running

   [3]  Set folder default
   [4]  Clear folder default

   [0]  Cancel

   Choose a number >
```

Selecting a session opens its source editor; selecting Set folder default opens a numbered session picker. Choosing Clear folder default saves the clear and returns to the overview. A folder without sessions still offers clearing when it has a stale saved default. `--default` requires `--name` or an exact full session name, while `--clear-default` accepts a folder or exact session target and cannot be combined with `--name`. Default changes never start, stop, or open a session.

After selecting `myenv1` as the default:

```sh
devbox open .                   # opens myenv1
devbox open . --name myenv2     # opens myenv2; default stays myenv1
```

All single-session lifecycle commands follow the same selection rule:

1. With a full session/container-name target, select that exact saved session independently of the current folder or folder defaults.
2. With a folder target and `--name`, select that name within the supplied folder.
3. With a folder target and no `--name`, select the folder's saved default.
4. If the requested session or default is unavailable, fail with an actionable message.

There is no implicit first-session, sole-session, or last-used fallback. Opening a session never changes the default. Commands never create a session merely because selection failed.

The default is a folder-level selection, separate from the sessions' config sources. `list .` shows all sessions, regardless of the default.

Deleting the default session clears the folder's default without selecting a replacement. Deleting only its container preserves both the saved session and the default. Default selection and session deletion must coordinate so concurrent operations cannot leave a default pointing to a deleted session.

Session lookup uses saved session identity and the folder's default, not config resolution. First select the saved session; load and validate its sources only when the requested operation needs them. Missing or broken sources must not prevent listing, setting a default, stopping, deleting, opening the source-chain editor, or showing saved status alongside configuration errors.

### Missing-default guidance

`devbox open .` without a selected default returns an error, even if the folder has only one session. It does not open a picker, choose a session, or create one. Follow the existing CLI's actionable error style and guide the user to set a default first:

```text
Error: No default session selected.
Target: .

Select a default session:
  devbox edit .
Then open it:
  devbox open .
```

If the folder has no sessions, guide the user through explicit creation first:

```text
Error: No sessions for this folder.
Target: .

Create a session:
  devbox create .
Then select a default:
  devbox edit .
Then open it:
  devbox open .
```

Preserve the entered folder spelling in hints. The same selection failures on other folder-targeted lifecycle commands use the shared error-guidance mechanism rather than introducing command-specific fallbacks.

## Config creation and editing

Create a config through its dedicated command:

```sh
devbox config create base
devbox config create ./devconfig
```

Use the same config-reference rules as `--config`. This command creates a new `config.json` and offers initial harness/artifact setup. An existing directory without `config.json` is allowed; preserve its existing files. If `config.json` already exists, fail before prompting or changing anything, even if that file is empty or invalid. Do not rerun setup or switch into the editor:

```text
Error: Config already exists.
Target: base

Edit the existing config:
  devbox config edit base
```

Use the entered reference in the repair command. Check for an existing `config.json` before prompting, then recheck under the config-directory owner lock before writing. Claim `config.json` using the existing no-replace publication helper before adding optional artifacts, so concurrent creation cannot overwrite or modify the winning creator's config.

Reuse the existing creation and initialization mechanisms rather than introducing a second implementation or a separate `config init` command. Do not open the existing-directory editor as another step of creation. Without a terminal, do not prompt or guess a harness; use the explicit setup options defined below, or create the minimal config when none are supplied.

Config creation neither creates a session nor selects a default. A newly created config only joins a session's source chain when the user selects it. Dedicated config copy, rename, and delete commands are outside this proposal; config directories remain ordinary filesystem directories.

### Config creation menus

Harness selection uses the shared numbered menu. A new config starts unset; returning from the optional-files step shows the pending harness choice. This is not a setup menu for an existing config. The unset option permits reusable overlays that do not choose a harness themselves:

```text
Select a harness

Current selection: Unset

   [1]  opencode
   [2]  pi
   [3]  Leave unset (selected)

   [0]  Cancel

   Choose a number >
```

Choosing a harness or explicitly leaving it unset advances to optional files. Do not force each source to choose a harness; validate that the combined session configuration selects one when creating or opening the session.

Select optional files one number at a time. Each submitted number toggles that item and redraws the menu with updated styling and selection summary. Do not require comma-separated numbers. This example shows two files already selected:

```text
Choose optional files

Current selection: Harness config files (pi), before-open.sh

   [1]  ✓ Harness config files (pi)
   [2]    setup.sh
   [3]  ✓ before-open.sh
   [4]    Dockerfile

   [5]  Continue

   [0]  Back

   Choose a number >
```

Keep item numbers stable while toggling. Selected names are emphasized and checkmarks are green; deselection removes the emphasis and checkmark. An empty selection is valid and displays `Current selection: None`.

Harness config files are available even when this config leaves its Harness setting unset. Turning that item on opens a numbered `Choose which harness's config files to add` picker. Show the pending/configured harness as the initial file-generation target when available, but allow choosing any available harness. Back cancels that unfinished selection. The chosen target appears beside the artifact item and in the selection summary; it controls only the generated directory and files, not the config's Harness setting. An overlay can therefore contain `pi/` files without selecting Pi for every session that uses it. Choosing another file-generation target does not change the persistent harness choice. Use the same artifact-target picker from Add optional files in `config edit`, not a second harness-settings editor.

Continue accepts the choices, creates the new config, and adds missing selected artifacts without replacing existing files. Back returns to harness selection with pending choices retained; toggling does not write files. Cancel from the first step creates nothing. After completion, print the created/kept file report and exit, without opening the settings editor or session creation. If artifact setup fails after `config.json` was created, report that the config now exists and direct the user to `config edit` to finish adding files; do not suggest repeating `config create`.

### Edit a config directory

Open the directory editor directly, whether or not any session references the config:

```sh
devbox config edit base
devbox config edit ./.devbox
```

This command uses the same config-reference rules as `config create` and `--config`. It edits settings and adds missing optional files only in the selected directory, not a session's source list or merged configuration. It requires an existing `config.json`; point the user to `config create` when the directory or config file is missing. Invalid existing configuration is a repair error, not permission to recreate or overwrite it.

Show the directory's actual path and other saved sessions referencing it before editing, separately from the compact settings dashboard. Resolve references against each session's workspace when finding shared use; do not compare reference text alone. Reuse the existing settings editor and its immediate-save behavior. Back only navigates within the editor or exits it. Source-selection menus must not provide another route into this editor.

The directory dashboard shows this source's settings over built-in defaults, not the combined configuration of an arbitrary referencing session. Use the existing table layout and styling, with generic source labels instead of profile/project roles:

```text
Config · base

        Setting                Value                 Source
   [1]  Harness                pi                    base
   [2]  Network                default               default
   [3]  Shell command          bash                  default
   [4]  Harness arguments      None                  default
   [5]  Docker options         None                  default
   [6]  Mounts
        • /data:/data:ro                              base

   [7]  Environment variables  None                  default
   [8]  Port forwards          None                  default
   [9]  VS Code extensions     None                  default
   [10] Base image             debian:bookworm-slim   default

   [11] Add optional files

   [0]  Done

   Choose a number >
```

Scalars retain the existing Edit value action, plus Remove this setting when configured. Use Remove this setting rather than Reset to inherited: a shared directory does not have one particular preceding session chain. Removing a setting removes the source key; it does not copy a resolved value into the file.

Lists open directly into the existing item actions, without an extra edit/approval submenu:

```text
Mounts configured here:
   [1]  /data:/data:ro

What would you like to do?

   [1]  Add mount
   [2]  Edit mount
   [3]  Remove mount
   [4]  Remove this setting

   [0]  Back

   Choose a number >
```

An absent list shows `No mounts configured here.` and only Add mount and Back. Show Remove this setting only when the source key exists. Text entry retains `New mount (:back cancels): `; successful edits print `Saved Mounts.` and return to the list. Do not add draft or confirmation stages.

Add optional files uses the shared toggle-selection control for harness config files, `setup.sh`, `before-open.sh`, and `Dockerfile`. It is an operation in the directory editor, not a route back into `config create` or its harness-selection workflow. Harness files use the separate file-generation target described above; do not require or change the directory's Harness setting just to add them. Continue adds only missing selected files under the existing config-directory owner lock, reports created/kept paths, and returns to the dashboard. Back cancels the pending file selection and returns without adding files. Existing files and settings are never overwritten by artifact setup; deselecting an item does not delete anything.

### Non-interactive config setup

Keep setup automation available on the same create/edit commands rather than requiring menus or retaining a separate `init` command:

| Flag on `config create` / `config edit` | Meaning |
|---|---|
| `--harness NAME` | Explicitly set the config's persistent `harness` field |
| `--artifact NAME` | Add missing files for the requested artifact; repeatable, retaining the existing comma-separated flag syntax too |
| `--artifact-harness NAME` | Choose which harness's files to generate, without setting or changing the config's `harness` field |
| `--json` | Print the operation result as JSON and never prompt |

Artifact names remain `harness-config`, `setup.sh`, `before-open.sh`, and `Dockerfile`. `--artifact-harness` requires `--artifact harness-config`. For harness-config generation, an explicit `--artifact-harness` takes precedence; otherwise use the config's own harness after applying an explicit `--harness`, if supplied. If neither provides a target, fail before changes with a hint to supply `--artifact-harness`. Do not infer it from a referencing session or a global default. Non-harness artifacts do not require a selected harness.

Supplying `--harness` or `--artifact` requests a direct operation even in a terminal: perform only the requested changes and do not open menus for omitted optional choices. With neither, interactive create/edit use their normal menus. Without a terminal or with `--json`, `config create` with no setup options creates only the minimal versioned config; `config edit` requires at least one explicit operation and otherwise returns an actionable error. `--json` is an output option, not an edit by itself.

Examples:

```sh
devbox config create base --harness pi --artifact Dockerfile --json
devbox config create ./overlay --artifact harness-config --artifact-harness pi
devbox config edit base --artifact setup.sh --artifact before-open.sh --json
devbox config edit ./overlay --artifact harness-config --artifact-harness opencode
devbox config edit base --harness opencode
```

The overlay creation example leaves `harness` unset. Adding OpenCode files later also leaves it unchanged; both harness file trees can coexist. Only the explicit `--harness` option changes that setting. Existing-file preservation, owner locks, validation, and conflict checks are the same for menus and flags. Validate requested options and artifact targets before mutation. Results retain created/kept paths, warnings, and next-step guidance, using JSON rather than human menu text when requested. Partial failures report completed changes rather than claiming rollback. These operations neither create sessions nor apply container changes.

### Folder overview

`devbox edit .` opens a menu of the folder's sessions, regardless of whether a default is selected:

```text
Select a session to edit

Folder: /work/api
Default: Main

   [1]  Main          default · stopped
   [2]  Experiment    running

   [3]  Set folder default
   [4]  Clear folder default

   [0]  Cancel

   Choose a number >
```

This screen lists sessions, not sources. The default annotation describes folder state; it does not preselect a session in this picker. Use the session list's running/inactive styling and keep annotations readable.

Selecting a session opens its source-chain editor. The overview changes the default: Set opens a numbered session picker; Clear needs no selected session. An empty overview guides the user to `devbox create .` unless a stale saved default needs clearing; it never implicitly creates a session.

Skip the overview when the session is already known:

```sh
devbox edit . --name main
devbox edit <full-container-name>
```

### Session source chain

Show each reference's workspace-relative or fixed form and its resolved path in saved order. A missing source stays visible with an error instead of preventing the menu from opening:

```text
Manage config sources

Session: Main
Folder:  /work/api

Sources, in order:
   1. base         fixed       ~/.devbox-neo/configs/base
   2. devconfig    relative    /work/api/devconfig
   3. personal     fixed       ~/personal

What would you like to do?

   [1]  Add source
   [2]  Replace source
   [3]  Remove source
   [4]  Reorder sources
   [5]  Show combined configuration

   [0]  Back

   Choose a number >
```

Back returns to the folder overview. When entered directly with `--name` or an exact session target, use Done instead because there is no previous picker. Default controls live only in the folder overview. Add/replace and reordering use the same source-selection controls described under session creation, but each completed operation here immediately saves the session's desired source chain. Hide remove/replace/reorder actions when there are no sources.

Show combined configuration uses the existing read-only renderer, including scalar and per-entry provenance, not a second editable settings dashboard.

Source rows describe references; they do not open directory editors. Show an exact `devbox config edit <reference>` command when the user needs to edit a source, using a path that resolves correctly from the invoking directory. Add source selects an existing config directory. Replace source lets the user repair a moved or missing directory reference without editing the other sources. Missing configs are created separately with `devbox config create <reference>`; source-chain operations never enter that workflow.

Distinguish these operations clearly:

- **`config edit <reference>`:** changes shared source files, affecting every session referencing that directory, potentially across folders.
- **Add, replace, remove, or reorder in `edit`:** changes only this session's desired source chain.
- **Show combined configuration in `edit`:** read-only inspection of the effective result and the sources contributing each value.

Do not present the merged result as an editable config, create hidden private copies, or write merged values into an arbitrary source. Completed edits save immediately, following the existing configuration menus; Back only navigates.

Allow incomplete configuration while editing. Validate the structure of each edit, but do not require every intermediate state to produce a runnable environment. The user may temporarily remove all sources or leave the combined configuration without a harness. Keep diagnostics visible and allow further repairs. `open`, creation, and recreation reject missing or invalid required configuration with a repair hint; `open` must not bypass this check just because the container is already running.

Non-interactive `--show` uses an exact full session name or a folder with explicit `--name`; it does not choose a folder session implicitly:

```sh
devbox edit . --name main --show
devbox edit . --name main --show --json
```

Managed runtime file changes synchronize before a stopped container starts, without requiring recreation. If the container is already running, stop it and start or open it again to apply those file changes. Settings and build/setup changes that require a new image or container remain unapplied until recreation. Inspect and apply those changes explicitly:

```sh
devbox status . --name main
devbox recreate . --name main
devbox open . --name main
```

## Config contents and composition

Every directory uses the same schema and artifact contract:

```text
config.json
Dockerfile
Dockerfile.dockerignore
setup.sh
before-open.sh
pi/
opencode/
tools/
```

Configs have no session `name`, no profile/project role, and no `inherit` cutoff. Every explicitly selected source participates in the saved order. Missing fields and artifacts contribute nothing.

Retain the settings/artifact composition rules from the config-directory design, without its profile/project selection model:

| Input | Rule |
|---|---|
| Scalars | Later explicit values replace earlier ones |
| Most lists | Append in source order, subject to field-specific validation |
| Environment assignments | Later assignment for the same variable wins |
| Shell argv | Later value replaces the complete argv |
| Configured harness arguments | Only sources explicitly naming the selected harness contribute |
| Dockerfiles | Build sequentially, each extending the preceding image through `DEVBOX_BASE` |
| `setup.sh` | Run in source order during container creation/recreation |
| `before-open.sh` | Run in source order before harness launch through `open` |
| Harness files | Overlay by relative path; later files win |

This is not blanket last-source-wins merging. Empty additive lists do not clear earlier entries; conflicting mounts or ports remain validation errors. No recursive config includes or generic list-removal language is proposed.

Preserve the config-directory proposal's image preparation, per-source build contexts and ignore rules, build arguments, harness finalization, cache reuse, and runtime validation contracts. Run lifecycle scripts as the development user with the workspace mounted and as the working directory. Scripts are separate processes; failure stops the chain, without rolling back completed effects.

## Saved state and application

The saved session owns its workspace, local name, ordered config references with their relative or fixed form, durable identity, and harness state. Its container can be replaced without creating a different session.

Editing a source or changing the source chain saves the desired configuration immediately. Opening or recreating uses those saved references, not config rediscovery or a new default-config selection. `status` compares them with the inputs actually applied to the container.

Synchronize the complete managed runtime file trees from the selected config directories, not just a whitelist of recognized harness config files. Additions, updates, and removal of obsolete managed files do not require recreation. Retain the existing startup contract: synchronize before starting a stopped container. Opening another harness in an already-running container does not synchronize files again; pending file changes wait for the next stop/start. Creation and recreation retain their existing synchronization path. Preserve the existing file-ownership rules: ordinary managed files are authoritative, shared JSON owns only its declared keys, and unmanaged files and harness history are not wiped. A changed mount or harness layout that cannot be applied to the existing container still requires recreation.

File synchronization is not image building or script execution. Dockerfiles and their build-context files affect the image; `setup.sh` effects apply during creation/recreation. Merely copying changed files does not apply those effects. `before-open.sh` uses the current ordered script chain before harness launch. Runtime-applicable settings continue to apply through the normal runtime path; settings requiring image/container changes wait for explicit recreation.

Recreation applies those image/container changes while retaining session identity and state. If it fails, keep the desired edits for repair and retry, and do not mark failed changes as applied. The existing committed inputs remain the recovery authority; this does not promise rollback of script side effects.

Missing or invalid required configuration blocks opening, creation, and recreation with actionable errors, never fallback to other sources. Selecting a saved session and repairing its source list do not require valid build configuration. Do not persist expanded secrets in merged settings snapshots.

## State storage and locking

Use the existing `store` package for saved session state and locks, and `resource` for editing config directories. Do not introduce a second session registry or put default selection into a workspace config directory. In the paths below, `<home>` is the selected Devbox home; this proposal does not change the rewrite's development-home isolation.

### Saved files

| Data | Location and contents |
|---|---|
| Session identity, desired sources, applied inputs | Existing `<home>/sessions/<full-name>/session.json` |
| Folder default | `<home>/state/workspaces/<workspace-key>.json` |
| Folder-default lock | `<home>/state/locks/workspaces/<workspace-key>.lock` |
| Session operation and record locks | Existing `<home>/state/locks/sessions/` paths |
| Config-directory edit locks | Existing `<home>/state/locks/config/` paths, keyed by the source directory's canonical path |

`<workspace-key>` is the full lowercase SHA-256 hex digest of the canonical absolute workspace path's bytes, following the existing path-keyed config-lock pattern. It is not the shortened hash in a container name. The default record contains `version: 1`, `workspace`, and `default_session`. A selected `default_session` contains the full session `name` and durable `id`; no selection is represented by `null`. An absent file also means no default. A malformed file is an error, not absence. Validate that the recorded workspace matches its key.

Create the workspace state record on the first default change, not during listing or config discovery. `edit --clear-default` writes `default_session: null` when a record exists and is a no-op when none exists. Do not store a duplicate list of sessions in this file. The name locates the selected record; the ID prevents a deleted-and-recreated session from inheriting a stale default merely because it reused the same name.

Keep the ordered desired source references in the session record's `sources` field. Each saved reference has a diagnostic `label`, a `kind` of `relative` or `fixed`, and a `path`. Relative paths are clean paths relative to the recorded workspace, including `.` or `..` where appropriate; fixed paths are clean absolute paths. Labels do not determine identity or resolution. Expand references to absolute source directories at the resolver boundary, so composition and build code retain their existing absolute-source contract. Applied inputs stay separate from these editable references. Session schema 5 also captures committed absolute source directories in `inputs.sources`; environment recovery validates against those directories rather than the mutable desired chain.

Record loading validates reference structure without requiring source directories to exist or contain valid config. An empty desired source list is valid saved state for repair, but cannot create, open, or recreate an environment. Do not persist a second mutable copy of the source chain in workspace state.

Use the existing atomic JSON publication helpers: write a private temporary file, sync it, rename it into place, and sync the parent directory. State files and lock files use `0600`; state directories use `0700`. Keep lock files external and stable; deleting a session or clearing a default never unlinks its lock.

### Lock ownership and operation order

Session operation locks remain the authority for session mutation. Preserve the existing sorted full-name order for operations holding multiple session locks. If an operation also needs folder-default locks, acquire them only after its session operation locks, in sorted workspace-key order when there is more than one. Never acquire a session operation lock while holding a folder-default lock. Existing short-lived record locks remain inside the store's record read/write/delete helpers.

- **Select a default:** choose the target without holding locks while prompting. Acquire its session operation lock, reload the record, and validate the selected session ID and workspace. Then acquire the folder-default lock and atomically save its name and ID. Validate saved state, not build configuration. Pending-transfer checks still apply.
- **Clear a default:** acquire only the folder-default lock and publish the cleared selection. Do not require a working container or load config sources.
- **Resolve a folder default:** read the default under its folder lock, then release that lock before acquiring any session operation lock. Treat the selection as a snapshot for this invocation. Reload and check the selected session's ID before mutation; if it was deleted or replaced, return an actionable selection error rather than substituting another session. A later default change does not retarget an already selected operation.
- **Edit a source chain:** retain the session ID and source list shown to the user. On submission, acquire the existing session operation lock and reload with `Locked.Load`. Reject a changed session ID or source list with a review-and-retry message, following the config editor's same-field conflict-check pattern. Update only `sources`, preserve the latest activity/applied fields, and save atomically. Do not hold locks while waiting for menu input or require idle harness commands merely to edit desired references.
- **Apply configuration:** load the saved sources and resolve them under the session operation lock. Do not resolve an old source-list snapshot before locking and then apply it after a concurrent edit. Editing a source chain does not change applied inputs or trigger synchronization itself; startup and recreation keep their existing application boundaries.
- **Edit config files:** retain the existing independent configuration-owner lock and same-field conflict checks. Shared-use reporting does not lock every referencing session, and config-directory editing does not acquire session or folder-default locks.

### Deletion and transfer

Creation uses the deterministic full session name and its existing operation lock to enforce folder-local name uniqueness. It needs no folder-default lock because it never sets a default. Inventory continues to come from saved session records; a missing or corrupt default must not hide those records or prevent exact-name session lookup. Exact-target default changes and deletion use the session's recorded canonical workspace as the state key; they do not require that workspace directory or its config sources to remain accessible.

Whole-session deletion retains the existing complete session-operation-lock set and preflight/confirmation rules. Immediately before removing a session's saved state, acquire its folder-default lock and clear the selection only if both name and ID match that session. Persist the clear successfully before deleting the session state, and release the folder lock after the clear; the session operation lock remains held through deletion, so `edit --default` cannot select the disappearing session. Container-only deletion, cancelled deletion, and dry runs do not clear defaults.

If deletion fails or the process exits after clearing the default but before removing state, the session may remain with no default. Report that partial result when possible; the user can select it again with `edit`. This ordering avoids dangling defaults without adding a cross-file transaction journal or automatically restoring an old choice over a newer one.

Centralize default clearing in the saved-session removal path rather than only the delete CLI handler. Committed move cleanup uses it too, including retries after the source record is gone; the existing transfer journal supplies the source workspace, name, and ID. A copy leaves the source default untouched. A move clears a matching source default when removing the source but does not select a destination default. Keep destination defaults unchanged and reuse the current endpoint locks and transfer-recovery machinery.

## Copy and move

Keep `copy` and `copy --move`, with explicit source and destination naming:

```sh
devbox copy . --name Main --as Experiment
devbox copy . /work/api-copy --name Main
devbox copy . /work/api-copy --name Main --as Review
devbox copy . --name Main --as Renamed --move
```

The command shape is `copy <folder|session> [destination-folder]`. Source selection follows the normal exact-target / folder-plus-`--name` / folder-default rules. `--as NAME` supplies the destination's folder-local name and uses the same validation as creation. Without it, preserve the source's local name. Without a destination folder, use the source's recorded workspace, not the invoking working directory; this supports same-folder copies and moves under another name. An identical source/destination identity is an error with a hint to choose `--as` or another destination folder.

A copy creates independent session identity and keeps the source. A move preserves durable session identity and removes the source only after the destination is committed, following existing transfer semantics. Same-folder moves with `--as` change the local/full name through that transfer path, not by editing a live record in place. Destination-name collisions fail; never overwrite an existing session or invent another name. Retain `--dry-run` and `--json` and the existing idle-state, running-intent, and recovery rules.

The source `--name` and destination `--as` selectors replace the old profile/project `--from` and `--to` slot selectors; do not retain those as aliases. The transfer journal pins both endpoint identities, including the chosen destination local name, so retries do not rediscover a different default or lose an explicit `--as`. Default-selection behavior is the same for same-folder and cross-folder transfers: copy leaves defaults alone; move clears a matching source default without selecting the destination.

Copy and move preserve each source reference's form and order:

- Workspace-relative references resolve against the destination workspace. A saved `devconfig` reference follows the workspace; a saved `../shared` reference follows the destination's relationship to its parent.
- Fixed references continue to use the same absolute directory. Do not automatically rebase a fixed path just because it was inside the original workspace.

This changes references, not the filesystem contract of copy/move: it does not implicitly copy or move workspace files or config directories. The user is responsible for making the referenced directories available at the destination. Show the destination's local/full name and resolved sources with their forms clearly, including in dry-run output.

Validate required destination sources before materializing its container. If a source later goes missing, opening also fails with its saved reference, resolved missing path, and a repair hint. For example:

```text
Error: Config source is unavailable.
Target: . --name main
Source: devconfig (workspace-relative)
Resolved path: /work/moved-api/devconfig

Replace or remove the source in the session's source chain:
  devbox edit . --name main
Then open the session:
  devbox open . --name main
```

The user can restore the directory, replace its reference, or remove it if the remaining chain is valid. Preserve the entered folder spelling in hints; exact session targets remain exact. If an old fixed path still exists, it remains the selected source: Devbox cannot infer that the user intended another directory.

## Acceptance scenarios

Implementation tests should cover these observable behaviors:

1. Two independently named sessions in one folder can share the same configs while keeping separate identity and harness state. The same local name can also be used in different folders.
2. A sole session is not implicitly the default. Opening never changes the default; deleting the default session clears it, while container-only deletion preserves it.
3. Config creation and directory editing have only their dedicated `config create` and `config edit` entry points. Session creation and `edit` never launch directory editors; `edit` changes the folder default only on an explicit action. Config creation does not create a session, select a source automatically, or set a default. Existing config files survive creation/setup unchanged.
4. Names of 1 and 64 characters are accepted; empty, 65-character, or invalid-character names fail without truncation or normalization. Uppercase letters, digits, dashes, and underscores work in local names. Case-distinct names produce distinct full names; symlink aliases of the same workspace produce the same name. Long or similarly sanitized folder basenames retain full-path identity through the hash. Duplicate local names in one folder fail, and duplicate canonical source directories are rejected.
5. Missing or broken config sources do not block session lookup, listing, default selection, stopping, deletion, or the repair menu. They do block opening, including into an already-running container, with a concrete repair hint.
6. Source edits, replacement, and reordering never rename the session. Incomplete intermediate edits can be saved and repaired, but cannot be used to open or recreate until valid.
7. Editing a shared source affects every referencing session. Managed runtime files synchronize before starting a stopped container, including new files and removal of obsolete managed files, without erasing unmanaged files/history. Opening into an already-running container does not synchronize again; a stop/start applies pending file changes without recreation. Image/container changes remain visible as unapplied until recreation.
8. Failed recreation retains desired edits without advancing the applied inputs for failed changes. Existing recovery and script-side-effect contracts remain intact.
9. Copy/move rebases workspace-relative references, preserves fixed references, and fails clearly on missing required sources or local-name collisions. `--as` permits same-folder copies/moves and explicitly named cross-folder destinations; omitted destination folders use the source workspace. Names are preserved when `--as` is absent, and identical source/destination identities fail rather than overwriting. Retried transfers retain the pinned source and destination, including an explicit destination name. Initial relative config arguments resolve against the invoking directory even when it differs from the workspace.
10. Concurrent default selection, source-chain edits, deletion, and move cleanup respect the session-before-workspace lock order. Deletion clears a matching default before removing state; container-only deletion, cancellation, and dry runs leave it unchanged. A stale default never selects a newly created session with a reused name. A stale source editor rejects changed sources/session identity rather than overwriting another completed edit; unrelated activity updates are preserved. Lifecycle operations resolve sources from the locked record, not a pre-lock snapshot.
11. Global environment and harness defaults no longer contribute to resolution. The explicit source chain and built-in defaults account for the effective environment.
12. First-time use works through `config create`, `create`, `edit`, and `open`. The session-name prompt starts blank and requires user input. Empty config pickers provide a creation command without opening a nested wizard, while still allowing selection of an existing directory path.
13. `config edit` works before any session references the directory. Top-level `edit` provides folder overview, exact-session access, source-chain editing, default selection/clearing, and combined `--show`/`--json` inspection without exposing a directory editor. The bare `config <folder|session>` and root-level `sources` forms are not retained.
14. All menus use the shared numbered layout, clear action titles, labeled context, wrapping, and terminal styling. Selection summaries agree with `(selected)` markers/checkmarks, including empty multi-selections. The folder editor names its default separately from the session being edited. Styling respects `NO_COLOR`, `TERM=dumb`, and redirected output without losing selection/status information or breaking alignment.
15. Optional artifacts toggle individually with stable numbers, visible selection updates, Continue, and Back. Back retains pending creation choices without writing files. Text input still uses `:back`; no raw-terminal or Escape controls are introduced. Completed settings/source-chain edits survive Back and EOF, while incomplete input is abandoned.
16. The folder editor lets users select a session to edit, or set/clear the folder default; Set opens a numbered session picker. The session editor manages sources only. Back returns to that picker, while directly targeted source editing ends with Done. Directory editors show only their own contributions over built-in defaults and never present another session's merged values as editable local settings.
17. An absent workspace-state file means no default without seeding one during reads. Corrupt default state is reported without hiding sessions or blocking exact session lookup. Failure after default clearing but before state removal leaves a recoverable session with no default, not a dangling selection. Copy/move retries preserve the specified source/destination default behavior.
18. Repeating `config create` for a directory with `config.json` fails before prompting or mutation and points to `config edit`, including for empty or invalid existing files. An existing directory without `config.json` is accepted without overwriting its files. Concurrent creators cannot replace one another's config or add artifacts after losing the creation race. Existing configs gain missing optional files through `config edit`; cancellation and deselection do not delete files, and completed additions preserve existing content and settings.
19. Harness-file generation works with an unset or different persistent harness. Its target picker and `--artifact-harness` generate the requested harness tree without altering `harness`. Only an explicit Harness edit or `--harness` changes that setting. General artifacts need no harness, and existing files survive repeated additions.
20. `config create` and `config edit` preserve scripted setup through `--harness`, repeatable/comma-separated `--artifact`, and `--json`, with a separate `--artifact-harness` target when needed. Explicit operations never open menus; missing required artifact targets fail before mutation. JSON contains the operation result without menu output. Minimal non-interactive creation is supported, while an edit with no operation fails clearly.
21. Bare config names and config pickers use `<selected-home>/configs` consistently for create, edit, and session source selection. Test the development default, `DEVBOX_HOME`, and overriding `--home`; no lookup falls back to the old `~/.devbox` home or another installation. Explicit filesystem references remain independent of the selected home.

## Scope

This is a replacement for profile/project-derived session selection, not another mode layered over it. Reuse existing lifecycle, locking, config editing, and ownership mechanisms where their contracts fit. The naming, persistence, and locking decisions above are part of this proposal, not unresolved user-facing choices.

No runtime migration, old-format readers, or compatibility layer is included. The existing standalone `devbox-migrate` utility is separately approved for adaptation to emit explicit configs and named sessions. Keep its old-installation parsing and conversion in the migration package; normal config/session loading reads only the new schema.
