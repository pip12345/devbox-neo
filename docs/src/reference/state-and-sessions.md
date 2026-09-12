# State — current development checkpoint

The default development home is `~/.devbox-neo`. Its important paths are:

| Path | Owner/purpose |
|---|---|
| `config.json` | Sparse global defaults; no implicit profile/harness |
| `profiles/<name>/` | Profile config and artifacts |
| `harnesses/<name>/` | User definition and optional defaults |
| `auth/<harness>/` | Managed persistent authentication |
| `cache/harnesses/<harness>/<store>/` | Shared declared harness caches |
| `sessions/<container>/session.json` | Durable identity, creation, launch, source verification, applied input snapshot, and activity |
| `sessions/<container>/active/` | Attached-command leases |
| `sessions/<container>/harnesses/<harness>/stores/<store>/` | Environment-scoped harness state |
| `sessions/<container>/harnesses/<harness>/managed-config.json` | Last applied file/key ownership and conflicts |
| `state/transfers/<source-container>.json` | Pending clone/relocate journal; reserves both endpoints |
| `state/installation-id` | Stable installation identity |
| `state/locks/installation.lock` | First-run initialization lock |
| `state/locks/config/*.lock` | Configuration-owner mutation locks |
| `state/locks/sessions/*.operation.lock` | Environment mutation lock |
| `state/locks/sessions/*.record.lock` | Short record read/write lock |
| `.build-*` | Temporary generated image context |

Profile/project creation stages a private `.devbox-create-*` directory beside its destination, then publishes it with a Linux no-replace rename. An interrupted staging directory is not a configured owner and is never adopted. Configuration locks remain outside the edited directories.

Docker creation uses a private `0600` env file in the OS temporary directory, removed when the create command returns. Its contents and temporary path are not persisted in session records. `env_sources` in the record holds only file/field/index references and keyed expression/value fingerprints. Invocation-only configured env is recorded as unrecoverable input, never copied into the record. Automatic terminal passthrough is separate: it adds no env-source references or fingerprints and is captured afresh from the invoking terminal during creation/recovery and attachment. Named external volumes must still exist for recovery.

Session records use schema version `2` and require `inputs.image`, `inputs.container`, and `inputs.runtime`. This snapshot contains public settings, source paths, file hashes/modes, and keyed env hashes—not file contents or env/auth values. Raw `--env` values are redacted in the diagnostic snapshot. The three fingerprints are derived from these inputs and validated against them. Image/container baselines advance only when creation/recreation commits; the runtime baseline advances with its applied fingerprint after successful synchronization. Status and pending-change warnings never advance either baseline.

Version-1 records and incomplete snapshots are rejected. Existing development containers and durable session records require a clean reset using the previous build before switching; save any needed session data separately. This is not the `session reset` command, which only clears selected harness stores. There is no compatibility reader, migration, automatic deletion, or guessed historical baseline. Global/profile/project config versions remain unchanged.

Locks remain outside removable session directories. Records are atomically replaced with restrictive permissions; corrupt state is not treated as absence. Container listings use the record's `last_activity` and `last_action`; `created_at` in list output comes from Docker's current container, not the durable session's creation timestamp. No additional activity state is introduced.

Container names are `devbox-<folder>-<12-hex-hash>.profile-<name>` or `devbox-<folder>-<12-hex-hash>.project`. The hash is derived from the full canonical workspace path and slot. The folder is the canonical path's basename, lowercased and limited to 32 characters from `a-z0-9_.-`. Invalid character runs become `-`; edge punctuation is trimmed. Empty results use `workspace`. Session directory names match container names. Earlier names without the folder or with `devbox-rewrite-` require a clean session reset; there is no automatic migration or deletion.

Containers carry installation, ownership-version, session, workspace, and slot labels under `devbox-rewrite.*`. Images carry installation ownership only. Final image tags are `devbox-rewrite/session:<session-id>`. Names are lookup keys, never proof of ownership.

Built-in mappings (targets are inside the container):

| Harness | Environment stores | Shared cache stores | Auth target |
|---|---|---|---|
| Pi | `home` → `/home/devuser/.pi/agent` | `npm-global` → `/home/devuser/.local`; `npm-cache` → `/home/devuser/.npm` | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | `config` → `/home/devuser/.config/opencode`; `data` → `/home/devuser/.local/share/opencode` | `cache` → `/home/devuser/.cache/opencode` | `/home/devuser/.local/share/opencode/auth.json` |

Parents of declared harness store/auth mounts are prepared in the filesystem that owns them: image-only ancestors as `devuser` during the runtime build, nested ancestors in their host backing source before create/start. Existing contents and ownership are not recursively changed. Unmounted paths, including OpenCode's `/home/devuser/.local/state`, remain container-local; writable parents do not imply persistence.

Runtime documentation and inspected network facts live inside the container under `/devbox`, not in durable session records. A private `.runtime-*` staging directory beneath the selected home exists only during copying and is removed afterward.

Reset clears selected environment-store contents only after complete stopped/idle preflight, preserving declared history unless `--include-history` is used. Bind-root directories and managed ownership manifests remain in place; the next normal open restores missing desired config. Auth and cache roots are separate and are not reset.

Container deletion retains session records/stores and image tags. Exact session deletion requires container absence and verifies tag ownership/association before removing state and its tag. External operation/record locks are not deleted. Filtered pruning requires a dry run or explicit `--yes`, and age is revalidated under lock.

`create` creates a new session and leaves its prepared container stopped; an existing session is never overwritten. Plain `open` and `start` require retained session state; `open --create` explicitly allows creation when absent. Recreation preserves the session ID and stores. Missing-container recovery uses the exact recorded image, mount layout, source definition, and setup input; it does not choose newer configuration. Both `open` and `start` retain this recovery behavior without requiring `--create`. Existing-container start/shell/exec do not require those old source inputs just to access the container.

Clone creates a new session ID; relocate preserves it. Transfers copy declared environment stores and their managed-config manifests, excluding auth overlays, shared caches, leases, and container-layer data. Opaque symlinks are copied without traversal; special files are rejected. The external journal contains public endpoint identities, IDs, mode, phase, intended running state, and input fingerprints, not env/auth values or another creation record. Completion removes it; no permanent lineage is kept.

The source installation's data is not imported automatically. The separate migration tool remains pending. See [commands](commands.md) and [progress](../../dev/progress.md).
