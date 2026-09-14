# Devbox rewrite

An independent, Linux-only Go scratch rewrite. **In development, not ready for cutover.**

- [Design and delivery phases](docs/dev/rewrite-plan.md)
- [Separate migration plan](docs/dev/migration-plan.md)
- [Implementation progress](docs/dev/progress.md)
- [Try the initial runtime](docs/src/guides/getting-started.md)

```sh
make install-go  # optional if the repo or parent already has .tools/go
make check
bin/devbox-neo --help
```

Saved environments are the top-level model: use `list`, `status <target>`, `status --all`, `clone`, and `relocate`. Single-target status combines saved session details, active commands, container state, and pending configuration changes. There is no `session` command group. List/status retain environments without containers and warn separately about unmatched managed containers.

`delete <target>` asks about container deletion, then saved data. Explicit `--container` deletes runtime only; `--session` deletes the whole environment, without prompts. Filter cleanup with `--older-than`, `--orphaned`, `--stopped`, or `--all`; preview with `--dry-run` and an explicit scope. `--force` never expands scope. There is no separate `prune` command.

Managed profile/project files are reapplied before any stopped-container `open`, `start`, `shell`, `exec`, or `ssh`, and during creation/recreation. Running access does not synchronize. Ordinary managed files overwrite local copies; Pi's shared JSON preserves undeclared keys. Unmanaged state/history remains intact. Invalid config blocks startup; no `reset` command is needed to restore managed files.

`ssh <target> <destination>` lets you authenticate in a host terminal and share the connection with the agent. The master runs inside the container by default; explicit `--host-master` uses host SSH configuration and prints a host/network-access warning. Keep the terminal open; Ctrl-C disconnects. No keys are copied. See the [SSH guide](docs/src/guides/ssh.md).

The development home defaults to `~/.devbox-neo`. `--home` overrides `DEVBOX_HOME`, which overrides that default. Selecting the old `~/.devbox` (or anything inside it) is rejected, including symlink aliases. Docker names and ownership labels also stay separate from the existing installation.

`make test-integration` requires a working local Docker daemon and uses isolated temporary homes and rewrite-only Docker resources. It must never target the existing installation.

## Browser documentation

```sh
make docs-build  # generate docs-html/index.html
make docs-serve  # preview at http://localhost:3000; Ctrl-C stops it
```

Requires Docker with a local daemon. Both commands run a pinned prebuilt Zensical image as your user; no local docs tools or package managers are needed. Only `docs/src/` is published. Preview reloads when docs change. Output (`docs-html/`) and cache (`.cache/`) are Git-ignored. Zensical's default browser assets may require internet. Normal CLI builds are unchanged.
