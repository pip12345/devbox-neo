# Trying the development rewrite

This is a Linux-only development build. The full rewrite is not complete; see [implementation progress](../../dev/progress.md). Use a non-root account with access to Docker.

## Build

From the rewrite repository:

```sh
make check
```

The binary is `bin/devbox-neo`. Its default home is `~/.devbox-neo`; container/session names include the folder and a short hash, such as `devbox-myapp-<hash>.profile-basic`, while Docker ownership labels and image tags retain the development namespace. `--home` overrides `DEVBOX_HOME`, which overrides the default. The old `~/.devbox` is rejected, including when an inherited environment variable selects it.

If upgrading from a build with version-1 session records, `devbox-rewrite-` names, or folder-less session names, clear the old containers and durable sessions using the old build before switching. This is a clean session reset, not a migration; save any needed session data separately. The new build does not rename or delete old resources automatically.

For Bash completion from this checkout:

```sh
export PATH="$PWD/bin:$PATH"
source <(devbox-neo completion bash)
```

The loaded script also enables completion for an existing `dbx` shortcut; it does not create the alias. Reload it after upgrading. Tab suggests profiles, harnesses, session/container targets, and flag values using the selected home. See [completion](../reference/commands.md#completion) for other shells.

## Configure the first profile

```sh
bin/devbox-neo profile create basic
bin/devbox-neo profile init basic --harness pi
bin/devbox-neo profile set basic
```

`profile create` writes sparse config without choosing a harness. `init` selects the harness; `set` makes the profile the default. No defaults are selected implicitly. Use `--harness opencode` for OpenCode.

In a terminal, `init` without flags offers harness and optional artifact choices. Init, profile selection, and config menus use consistent numbered choices with bold headings and subdued help text. For automation, use explicit flags:

```sh
bin/devbox-neo profile init basic --harness pi --artifact harness-config,setup.sh
bin/devbox-neo profile list
```

Re-running init keeps existing files. Optional artifacts are harness config, `setup.sh`, `entrypoint.sh`, and `Dockerfile`.

Use `bin/devbox-neo profile config basic` to edit settings in a numbered menu. Choose a setting and edit its value or reset it to inherited. Each valid operation saves immediately—there is no separate save or confirmation step. The overview and `--show` print list entries underneath each setting, wrapping long values instead of truncating them. The source column shows `default`, the edited layer (`global`, `profile`, or `project`), or `inherited - global` / `inherited - profile`. Lists show a source beside each entry instead of one combined label. Lists open directly: an empty list offers **Add**, while existing entries can be edited or removed. The overview includes inherited entries; the list editor shows only entries in the selected profile/project, which are the only ones it can change. Press Enter to submit a value. Use `0` to go back or exit, or `:back` to cancel text entry; leaving the menu does not undo completed changes. The same menus are available through `global config` and `project config <folder>`. Input is visible; use `${env:NAME}` references rather than typing credentials.

For custom Pi providers, add `pi/models.json` to your profile or project `.devbox/` directory. Devbox synchronizes its `providers` object before starting a stopped container, preserving other live top-level keys.

Create an environment, then open it:

```sh
bin/devbox-neo create /path/to/workspace --profile basic
bin/devbox-neo open /path/to/workspace --profile basic
```

`create` builds the image and runs preparation/setup, then leaves the container stopped without launching Pi. It refuses an existing session. `open` starts the existing environment and launches its harness. Container options such as `--network`, `--env`, and `--harness` belong to `create` or `recreate`, not `open`. Plain `open` and `start` never create a new session. If none exists, the error suggests `create` with the folder you entered. This generic hint uses normal configuration selection, without profile flags. The default `on_exit` policy stops the container after the last attached Devbox command exits.

Find recently used saved environments with `bin/devbox-neo list --sort last-active`, including ones whose containers were deleted. The table shows name, harness, profile, activity, container state, and folder. Stopped and missing rows are subdued so running containers stand out; error and pending-transfer details remain readable. Add `--wide` for exact activity/creation times and the last action. Containers with no session record appear as warnings below the table. JSON has separate `sessions` and `unmatched_containers` arrays.

Listing does not clean up state. Use `delete --session --orphaned --dry-run` to preview cleanup, adding `--older-than 720h` when you only want sessions inactive for more than 30 days. Remove `--dry-run` to delete the matches without prompting. These commands are top-level; there is no `session` command group.

For a non-interactive launch check without provider credentials:

```sh
bin/devbox-neo open /path/to/workspace --profile basic -- --version
```

## Configure a project instead

```sh
bin/devbox-neo project create /path/to/workspace
bin/devbox-neo project init /path/to/workspace --harness pi
bin/devbox-neo create /path/to/workspace
bin/devbox-neo open /path/to/workspace
```

Use `--harness inherit` on project init to use the participating profile/global harness. Inheritance must resolve to a configured harness.

To start a standalone project from a reusable profile:

```sh
bin/devbox-neo project create /path/to/workspace --from-profile basic
```

This copies the profile's supported source artifacts once and sets `inherit_profile: false`. It preserves variable expressions, refuses an existing `.devbox/`, and does not track later profile changes. Global defaults still apply. Use `profile set --clear` to clear the default profile.

## Customize the image

```sh
bin/devbox-neo profile init basic --artifact Dockerfile
```

The standard runtime already includes vim, zip, unzip, jq, net-tools (`ifconfig`), and iputils-ping (`ping`), alongside Bash, git, curl, sudo, and procps. Interactive Bash has `ll='ls -alF'` and `vi='vim'`.

Edit the generated Dockerfile to add other tools to a Debian-compatible base. Its directory is the build context, so `COPY` can use sibling files. Use `.dockerignore` to exclude files that are not image inputs. Devbox always installs its runtime and harness afterward; there is no full override mode. Init never replaces an existing Dockerfile.

The runtime layer prepares writable parents for declared harness mounts. If a custom base has incompatible permissions on those parents, correct the base image; do not recursively change ownership of mounted session/auth data. Use `recreate` to apply image-layer fixes to an existing environment.

## Apply changes explicitly

Check which saved environments need attention, including those with missing containers:

```sh
bin/devbox-neo status --all
bin/devbox-neo status --all --profile basic
```

The table separates running/stopped/missing container state from configuration health. A missing container is not automatically an error. Reasons below each affected environment explain which settings or files changed. For one environment, use `status <name>`. Containers without session records are reported as warnings. `Rebuild + recreate needed` means image inputs changed; `Recreate needed` means only container inputs changed. `Runtime changes` do not need a rebuild. `Cannot check` means the diagnostic needs attention, not that the container is up to date. This checks local inputs, not newer upstream package or base-image releases.

Valid creation changes warn instead of replacing the existing container. On `open`, specific reasons such as `network: default -> host`, changed Dockerfile/build-context paths, or changed environment variable names appear first, before startup and entrypoint output. Env values and file contents are not shown. Opening continues immediately with the existing creation settings. Apply changes explicitly:

```sh
bin/devbox-neo recreate /path/to/workspace --profile basic
bin/devbox-neo recreate /path/to/workspace --profile basic --image
```

Ordinary `recreate` replaces the container using current configuration. It reuses the recorded image when image inputs are unchanged and the image is available; otherwise it builds with caching enabled. This includes changes to Devbox's bundled tools and aliases after a binary update. `--image` forces a no-cache build even when inputs are unchanged. It does not promise to refresh upstream base images. Durable harness state is preserved; changes made only inside the old container are lost.

Managed config is synchronized before startup, whether you use `open`, `start`, `shell`, or `exec`. Creation/recreation synchronizes too. If the container is already running, access commands leave its managed files alone. To apply deferred changes, stop it when safe and start it through any access command.

Profile/project-managed files are authoritative. Local edits to their container copies are overwritten at the next synchronization, even if the source did not change. Edit the profile/project for durable changes. Pi's shared JSON files still merge only Devbox-owned keys, preserving Pi's other settings. Unmanaged files and conversations remain untouched. There is no `reset` command.

Harness config copies warn and skip symlinks and other non-regular entries. Opening continues, but skipped files are not supplied by that source. If an extension needs skipped `node_modules/.bin` links, install its dependencies inside the container; copying the source tree does not preserve those links.

Pi starts in fullscreen TUI mode by default (an experimental upstream mode). To use regular mode for one open:

```sh
bin/devbox-neo open /path/to/workspace --profile basic -- --tui-mode regular
```

For a persistent override, set the profile/project `harness_args` to `["--tui-mode", "regular"]`. Recreate existing recorded Pi environments to adopt the new built-in default. A cached image with an older Pi that does not support `--tui-mode` needs an explicit `recreate --image` to reinstall Pi; ordinary recreation does not promise upstream updates.

## Shell and command access

```sh
bin/devbox-neo start /path/to/workspace --profile basic
bin/devbox-neo shell /path/to/workspace --profile basic
bin/devbox-neo exec /path/to/workspace --profile basic -- git status
bin/devbox-neo stop /path/to/workspace --profile basic
```

Failures now include a code, the failed operation, and safe next commands when available. Follow the suggested command, then retry; Devbox does not run repairs or force flags automatically. Commands already supporting `--json` also return structured failures on stdout with a nonzero exit code.

Starting a stopped container resolves current profile/project configuration and synchronizes managed runtime files first. Invalid config or malformed live shared JSON blocks startup, including a shell; errors are not silently ignored. Attaching with `shell`/`exec`, or calling `start` on an already-running container, does not resolve or synchronize config. `stop` never synchronizes.

`shell` and `exec` require an existing container. If saved state remains but its container is missing, `open` and `start` recover recorded creation settings when their inputs remain available, applying compatible current runtime config before startup. Otherwise use `recreate`. An incompatible harness/layout change also needs recreation. New environments must be created with `create`.

Shells and harness launches receive your current terminal's `TERM`, `COLORTERM`, and related display settings. Reconnect with `shell` to pick up terminal changes; no recreation or Bash config edit is needed for forwarding. Devbox does not import your host prompt or dotfiles.

## Copy or move a session

Prepare the destination folder and its profile/project configuration first. Transfers move harness state, not workspace files or installed container-layer tools.

```sh
bin/devbox-neo stop /path/to/workspace --profile basic
bin/devbox-neo clone /path/to/workspace /path/to/copy --dry-run
bin/devbox-neo clone /path/to/workspace /path/to/copy
bin/devbox-neo relocate /path/to/workspace --from basic --to .project
```

Use an exact container name when the source folder has multiple slots. `.project` requires an initialized project; named destination profiles must exist. Clone requires a stopped or absent source container and leaves the destination stopped. Relocate can stop a running source, then restore that running state at the destination after preparation succeeds.

If a transfer is interrupted, inspect `list` or `show <exact-name>`, fix the reported problem, and retry the same transfer command. Do not delete pending state manually. Before commitment, restore changed destination inputs before retrying; after commitment, retry only finishes recovery/cleanup.

## Delete an environment

Run `bin/devbox-neo delete <target>` in a terminal. It first asks to delete the container, then separately asks whether to delete saved session data and conversation history. Answer no to the second prompt to retain an environment that can be recovered later. If its container is already missing, only the saved-data prompt is needed.

For scripts, choose the deletion scope explicitly:

```sh
bin/devbox-neo delete <target> --container # runtime only; keep saved state
bin/devbox-neo delete <target> --session   # whole environment, including history
bin/devbox-neo delete --session --orphaned --older-than 720h --dry-run
```

Explicit `--container` or `--session` scope skips prompts; they are mutually exclusive. A scope is required for scripts, JSON output, and dry runs. `--force` permits disrupting attached container commands but never expands scope; saved-data deletion still requires an idle session. Combine `--stopped`, `--orphaned`, and `--older-than` to narrow selection, or use `--all`. Age is last recorded Devbox activity and is rechecked under lock. Exact targets cannot be combined with selection filters. Workspace files, configuration, managed auth, and shared caches are not deleted. There is no separate `prune` command.

## Help inside the container

Read `/devbox/AGENTS.md` for the container contract and `/devbox/docs/index.md` for the user docs. The built-in Pi/OpenCode `devbox` skill points to these files. Init does not copy it into your configuration. To override it explicitly, add `<harness>/skills/devbox/SKILL.md` under your profile or project `.devbox/` directory. To inspect network facts in a shell, run `cat /devbox/network/inspect.json` or source `/devbox/network/env`. These files are managed by Devbox, not editable project configuration.

## Environment and other creation settings

Profile/project config can reference the host environment:

```json
{
  "version": 1,
  "harness": "pi",
  "extra_env": ["WORK_TOKEN=${env:WORK_TOKEN}"],
  "extra_mounts": ["${env:HOME}/data:/data:ro"],
  "extra_ports": ["127.0.0.1:8080:8080"]
}
```

References expand once per operation; unset references are errors. Env values are sensitive. Paths, names, argv, and other ordinary settings are public, so do not put credentials there. Env values must be single-line. Managed Pi/OpenCode auth persists at `~/.devbox-neo/auth/<harness>/auth.json`.

Inspect effective values and provenance without starting Docker:

```sh
bin/devbox-neo profile config basic --show
bin/devbox-neo project config /path/to/workspace --show --json
```

Env values are redacted. Global/profile/project env can be recovered from verified source entries after container loss. One-off `--env KEY=VALUE` has no durable source: use explicit `recreate --env KEY=VALUE` after container loss. Existing-container start/shell/exec do not need the old env inputs.
