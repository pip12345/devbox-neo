# State — current development checkpoint

The default development home is `~/.devbox-neo`. Its important paths are:

| Path | Owner/purpose |
|---|---|
| `config.json` | Sparse global defaults; no implicit profile/harness |
| `profiles/<name>/` | Profile config and artifacts |
| `harnesses/<name>/` | User definition and optional defaults |
| `auth/<harness>/` | Managed persistent authentication |
| `cache/harnesses/<harness>/<store>/` | Shared declared harness caches |
| `sessions/<container>/session.json` | Durable identity, creation, launch, source verification, and activity |
| `sessions/<container>/active/` | Attached-command leases |
| `sessions/<container>/harnesses/<harness>/stores/<store>/` | Environment-scoped harness state |
| `sessions/<container>/harnesses/<harness>/managed-config.json` | Last applied file/key ownership and conflicts |
| `state/installation-id` | Stable installation identity |
| `state/locks/installation.lock` | First-run initialization lock |
| `state/locks/config/*.lock` | Configuration-owner mutation locks |
| `state/locks/sessions/*.operation.lock` | Environment mutation lock |
| `state/locks/sessions/*.record.lock` | Short record read/write lock |
| `.build-*` | Temporary generated image context |

Profile/project creation stages a private `.devbox-create-*` directory beside its destination, then publishes it with a Linux no-replace rename. An interrupted staging directory is not a configured owner and is never adopted. Configuration locks remain outside the edited directories.

Docker creation uses a private `0600` env file in the OS temporary directory, removed when the create command returns. Its contents and temporary path are not persisted in session records. `env_sources` in the record holds only file/field/index references and keyed expression/value fingerprints. Invocation-only env is recorded as unrecoverable input, never copied into the record. Named external volumes must still exist for recovery.

Locks remain outside removable session directories. Records are atomically replaced with restrictive permissions; corrupt state is not treated as absence.

Containers carry installation, ownership-version, session, workspace, and slot labels. Images carry installation ownership only. Final image tags are `devbox-rewrite/session:<session-id>`. Names are lookup keys, never proof of ownership.

Built-in mappings (targets are inside the container):

| Harness | Environment stores | Shared cache stores | Auth target |
|---|---|---|---|
| Pi | `home` → `/home/devuser/.pi/agent` | `npm-global` → `/home/devuser/.local`; `npm-cache` → `/home/devuser/.npm` | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | `config` → `/home/devuser/.config/opencode`; `data` → `/home/devuser/.local/share/opencode` | `cache` → `/home/devuser/.cache/opencode` | `/home/devuser/.local/share/opencode/auth.json` |

Reset clears selected environment-store contents only after complete stopped/idle preflight, preserving declared history unless `--include-history` is used. Bind-root directories and managed ownership manifests remain in place; the next normal open restores missing desired config. Auth and cache roots are separate and are not reset.

Container deletion retains session records/stores and image tags. Exact session deletion requires container absence and verifies tag ownership/association before removing state and its tag. External operation/record locks are not deleted. Filtered pruning requires a dry run or explicit `--yes`, and age is revalidated under lock.

Recreation preserves the session ID and stores. Missing-container recovery uses the exact recorded image, mount layout, source definition, and setup input; it does not choose newer configuration. Existing-container start/shell/exec do not require those old source inputs just to access the container.

The source installation's data is not imported automatically. Session list/show/reset/prune/delete are available; transfer commands and the separate migration tool remain pending. See [commands](commands.md) and [progress](../../dev/progress.md).
