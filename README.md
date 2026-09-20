# Devbox rewrite

An independent, Linux-only Go scratch rewrite. **In development, not ready for cutover.**

- [Design and delivery phases](docs/dev/rewrite-plan.md)
- [Environment identity and configuration model](docs/dev/environment-model-plan.md)
- [Generic sources beneath profile/project customization](docs/dev/config-directory-proposal.md)
- [Separate migration plan](docs/dev/migration-plan.md)
- [Implementation progress](docs/dev/progress.md)
- [Try the initial runtime](docs/src/guides/getting-started.md)

```sh
make install-go  # optional if the repo or parent already has .tools/go
make check
bin/devbox-neo --help
```

Saved environments are the top-level model: use `list`, `status <folder|session>`, `status --all`, and `copy` (add `--move` to remove the source after copying). Single-target status combines saved session details, active commands, container state, and pending configuration changes. There is no `session` command group. List/status retain environments without containers and warn separately about unmatched managed containers.

`delete <folder|session>` asks about container deletion, then saved data. Explicit `--container` deletes runtime only; `--session` deletes the whole environment, without prompts. Filter cleanup with `--older-than`, `--orphaned`, `--stopped`, or `--all`; preview with `--dry-run` and an explicit scope. `--force` never expands scope. There is no separate `prune` command.

Config sources share a generic `inherit` cutoff; environment naming stays in profile/project selection. Profiles/projects remain the public interface, with project configuration in the workspace's `.devbox/` directory. Dockerfiles and setup/before-open scripts chain in source order. Devbox prepares the development user before custom Dockerfiles extend `DEVBOX_BASE`, then installs the harness last. Session schema 4 requires a clean development-state reset; there are no old-record readers.

Managed profile/project files are reapplied before any stopped-container `open`, `start`, `shell`, `exec`, or `ssh`, and during creation/recreation. Running access does not synchronize. Ordinary managed files overwrite local copies; Pi's shared JSON preserves undeclared keys. Unmanaged state/history remains intact. Invalid config blocks startup; no `reset` command is needed to restore managed files.

`ssh <folder|session> <destination>` lets you authenticate in a host terminal and share the connection with the agent. The master runs inside the container by default; explicit `--host-master` uses host SSH configuration and prints a host/network-access warning. Keep the terminal open; Ctrl-C disconnects. No keys are copied. See the [SSH guide](docs/src/guides/ssh.md).

The development home defaults to `~/.devbox-neo`. `--home` overrides `DEVBOX_HOME`, which overrides that default. Selecting the old `~/.devbox` (or anything inside it) is rejected, including symlink aliases. Docker names and ownership labels also stay separate from the existing installation.

`make test-integration` requires a working local Docker daemon and uses isolated temporary homes and rewrite-only Docker resources. It must never target the existing installation.

## Migration utility

`make build-migrate` builds the separate `bin/devbox-migrate` utility. Run `bin/devbox-migrate` in a terminal to choose:

- `[1]` **Preview migration (read-only)** — the same metadata preview as `--dry-run`, without copying files or creating staging.
- `[2]` **Prepare staged copy** — prepare a separate copy without changing either installation's data.
- `[3]` **Review and import** — resolve conflicts and approve the import.
- `[4]` **Resume** — continue previously approved work without resetting completed imports.
- `[0]` **Exit**.

The landing menu shows source/destination paths and short availability labels. Action details, warnings, and confirmation prompts appear after selection. Preview remains available even if staging already exists or cannot be read. In **Choose what to stage**, each row shows include/skip separately from its metadata status, using the preview's terminal colors. **Exclude all items** turns every item and caches off; toggle individual items back on as needed. Copying into staging never automatically authorizes importing into Neo. Explicit `--stage`, `--merge`, and `--resume` entry points remain available; `--dry-run` is read-only and `--help` lists scripting flags. Without terminal input, the bare command prints help instead of prompting. Inventory output groups items by kind, summarizes their status, and prints review/conversion notes directly under each affected item. Session/profile blocks have blank-line separation, bold headings, and colored status labels in terminals. Redirected output and saved reports stay plain; `NO_COLOR` and `TERM=dumb` disable styling. Repeated notes use the same numbers throughout the report, sorted ascending on each item, with a numbered summary below. Errors and warnings stay visible on each item. Use `--verbose` for full paths, identities, activity, and lineage; saved `report.txt` files always retain full details. Discovery reads metadata, not conversation/config-payload/cache trees. `[Metadata OK]` means metadata checks passed—not that payload files or links were checked. `Planned staging path` is a future output location; dry-run creates nothing. After approval, staging fully scans and verifies only selected data, with progress output; sizes and deep-tree errors are reported then. Caches are not scanned unless selected. Errors identify failed schema checks and paths while withholding config values.

The [source compatibility table](docs/dev/migration-plan.md#supported-source-formats) covers global config v1/v2, sparse and legacy layer fields, metadata v3/v4, and pre-label sessions. Both flat stores and nested `harnesses/` stores with verified layout aliases are supported. Retained Claude/Codex/Copilot stores produce explicit warnings and stay in the original installation; they do not block importing a Pi/OpenCode session. Removed settings and stale generated links require explicit merge acceptance. Arbitrary user directory names have no special meaning.

**This is a development candidate; real-Docker and conversation/auth acceptance remain unpassed.** Read the [migration contract and safety limits](docs/dev/migration-plan.md#current-implementation) before using it. Tests use temporary homes and fake Docker, not personal installations.

## Browser documentation

```sh
make docs-build  # generate docs-html/index.html
make docs-serve  # preview at http://localhost:3000; Ctrl-C stops it
```

Requires Docker with a local daemon. Both commands run a pinned prebuilt Zensical image as your user; no local docs tools or package managers are needed. Only `docs/src/` is published. Preview reloads when docs change. Output (`docs-html/`) and cache (`.cache/`) are Git-ignored. Zensical's default browser assets may require internet. Normal CLI builds are unchanged.
