# Working on Devbox Neo

This directory is a separate Git repository and Go module. The executable is `devbox-neo`; `dbx` is only an optional user alias. Support Linux only.

## Build and test

- Use the local toolchain: `make` finds `.tools/go` here or in the parent. Run `make install-go` if needed. Minimum Go: 1.24.2.
- Finish implementation and review before running `make test-fast`. Fix failures and rerun affected tests. Use `make test` instead when disk-backed app temporary files matter; do not run both by default.
- `make build` builds the CLI; `make build-migrate` builds the separate importer. `make check` adds formatting and race checks.
- Prose-only changes need no tests. Changes to embedded runtime assets require `make test-fast`.
- Tests use temporary homes and fake Docker resources, never the user's state. Report live-Docker and manual acceptance separately from automated results.

## Before changing a subsystem

Read its architecture page and nearby tests. Keep behavior with its existing owner; do not duplicate lifecycle logic in menus or command handlers.

| Work | Read |
|---|---|
| Package boundaries, errors | `docs/src/architecture/runtime.md` |
| Creation, startup, recovery | `docs/src/architecture/lifecycle.md` |
| Configs, harness definitions, images | `docs/src/architecture/configuration.md` |
| Identity, locking, deletion, transfers | `docs/src/architecture/state.md` |
| Terminal UI and cancellation | `docs/src/architecture/interactive.md` |
| SSH sharing | `docs/src/architecture/ssh.md` |

Approved design decisions and acceptance history live in `docs/dev/`. The named-session/config contract is `generic-config-alternative.md`; the importer follows `migration-plan.md`.

## Preserve these boundaries

- Names locate resources; installation/ownership labels authorize changes. Mutations use external locks. Missing state is not corrupt state.
- Creation is explicit and leaves a stopped session. Never choose a folder default or create a session implicitly.
- Config references are explicit and ordered. Desired config is separate from applied state; execution uses captured inputs, not another config read.
- Harness behavior belongs in definitions, not harness-name branches. Managed files replace their owned content, never unrelated history.
- Deletion retains scope, confirmations, idle checks, and locks across phases. Transfer retries retain exact endpoints and never recopy after commitment.
- The CLI is the source of truth. The interactive menu must fit the CLI's behavior, not the other way around. The CLI must remain fully usable without ever opening an interactive menu.
- The UI calls shared services. Keep one terminal reader, immutable display snapshots, graceful cancellation, and foreground handoff.
- Preserve error causes and use `commanderror.Error`/`Step` for guidance. Do not print secrets or persist env/auth values in records.
- No unapproved compatibility readers, migrations, aliases, or automatic adoption. Importer migration code stays in dedicated `migrations.go` files.

## Documentation discipline

Write for the reader's next task, not to catalogue the implementation.

- **Guides:** one concept at a time, simplest working path first. Explain notation and show what happens next. Put alternatives and edge cases later or link to reference.
- **Reference:** scannable syntax, defaults, fields, and constraints. Do not repeat walkthroughs or implementation rationale.
- **Architecture:** contributor-facing ownership, ordering, invariants, and reasons.
- **Installed `/devbox/AGENTS.md`:** essential instructions for an agent inside the container. No host-UI walkthrough, CLI inventory, schema history, or release notes.

Keep real warnings where the user makes the relevant decision. Verify examples against current commands. Update affected pages together, but do not copy each behavior change into every tier or either agent guide. Use “harness” for the coding tool and “config” for user-selected settings.

Keep this file short. Add subsystem detail to its architecture page, not another paragraph here. Record delivery status in `docs/dev/progress.md`.
