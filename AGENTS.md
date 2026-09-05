# Rewrite contributor guide

This is a separate Git repository and Go module. `docs/dev/rewrite-plan.md` is the runtime contract; `docs/dev/migration-plan.md` is a separate removable utility, not startup behavior.

- Support Linux only. Use Linux process identity, terminal, and locking APIs directly; do not add macOS/Windows fallbacks or portability abstractions.
- Run `make test` after changes; `make check` also formats, race-tests, and builds.
- `make` prefers this repository's `.tools/go`, then the parent's toolchain. Use `make install-go` for a standalone checkout.
- Development builds are named `devbox-neo`. Their default home is `~/.devbox-neo`; `--home` takes priority over `DEVBOX_HOME`. The conventional old `~/.devbox` and its descendants are rejected even when explicitly selected. Docker resources use separate `devbox-rewrite` names and ownership labels. Tests must use temporary homes, never the user's current home or Docker resources.
- Keep lifecycle generic: no harness-name branches, old-schema readers, or config reloading in execution.
- Images have one ownership contract: optional user Dockerfile base, then Devbox runtime and harness installation. Do not reintroduce `Dockerfile.full` or a user-owned runtime branch.
- Session mutation requires external operation locks; labels, not names, prove Docker ownership.
- `resource` owns profile/project creation and source edits. Use external configuration-owner locks and no-replace publication for new directories/files. `create` stays sparse; `init` never refreshes existing artifacts. Source copies preserve expressions and use `artifact.SourceTree`; project inheritance previews use the normal resolver.
- Never persist secrets in session records or print expanded configuration in diagnostics.
- Keep tests and guide/reference/architecture docs aligned. Record unpassed acceptance gates honestly in `docs/dev/progress.md`.
- Migration logic belongs only in dedicated `migrations.go` files inside the removable migration package.
