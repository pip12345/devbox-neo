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

Version-1 records and incomplete snapshots are rejected. Existing development containers and durable session records require a clean reset using the previous build before switching; save any needed session data separately. There is no compatibility reader, migration, automatic deletion, or guessed historical baseline. Global/profile/project config versions remain unchanged.

Locks remain outside removable session directories. Records are atomically replaced with restrictive permissions; corrupt state is not treated as absence. Environment listings use the record's `last_activity` and `last_action`; `created_at` in list output comes from Docker's current container, not the durable session's creation timestamp. No additional activity state is introduced.

Container names are `devbox-<folder>-<12-hex-hash>.profile-<name>` or `devbox-<folder>-<12-hex-hash>.project`. The hash is derived from the full canonical workspace path and slot. The folder is the canonical path's basename, lowercased and limited to 32 characters from `a-z0-9_.-`. Invalid character runs become `-`; edge punctuation is trimmed. Empty results use `workspace`. Session directory names match container names. Earlier names without the folder or with `devbox-rewrite-` require a clean session reset; there is no automatic migration or deletion.

Containers carry installation, ownership-version, session, workspace, and slot labels under `devbox-rewrite.*`. Images carry installation ownership only. Final image tags are `devbox-rewrite/session:<session-id>`. Names are lookup keys, never proof of ownership.

Built-in mappings (targets are inside the container):

| Harness | Environment stores | Shared cache stores | Auth target |
|---|---|---|---|
| Pi | `home` → `/home/devuser/.pi/agent` | `npm-global` → `/home/devuser/.local`; `npm-cache` → `/home/devuser/.npm` | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | `config` → `/home/devuser/.config/opencode`; `data` → `/home/devuser/.local/share/opencode` | `cache` → `/home/devuser/.cache/opencode` | `/home/devuser/.local/share/opencode/auth.json` |

Parents of declared harness store/auth mounts are prepared in the filesystem that owns them: image-only ancestors as `devuser` during the runtime build, nested ancestors in their host backing source before create/start. Existing contents and ownership are not recursively changed. Unmounted paths, including OpenCode's `/home/devuser/.local/state`, remain container-local; writable parents do not imply persistence.

Runtime documentation and inspected network facts live inside the container under `/devbox`, not in durable session records. A private `.runtime-*` staging directory beneath the selected home exists only during copying and is removed afterward.

Managed configuration synchronizes before ordinary startup, including open/start/shell/exec and creation/recreation. Ordinary managed files overwrite their live copies; undeclared shared JSON keys, unmanaged files, and history remain. Running access, inspection, stopping, and deletion do not synchronize harness files. There is no broad harness-state reset command. The obsolete harness-definition `reset_preserve` field is rejected; remove it from custom definitions and recreate environments to adopt changed definitions.

`list`, `status`, `show`, `clone`, `relocate`, and `delete` are top-level environment commands. List/status inventory includes retained sessions without containers and warns separately about unmatched installation-owned containers; corrupt records remain diagnostic session rows. `list --json` and `status --all --json` have `sessions` and `unmatched_containers` arrays.

Container deletion retains session records/stores and image tags unless saved-data deletion is explicitly selected. Interactive `delete` without scope asks about containers first and saved state afterward. Mutually exclusive `--container` and `--session` scopes respectively select runtime-only and whole-environment deletion without prompts. Explicit scope is required for scripts, JSON output, and dry runs. `--force` only relaxes attached-command protection for container removal; saved-data deletion still requires idle sessions. The complete lock set remains held across both phases. Container absence and image-tag ownership/association are verified before saved data and its tag are removed. External operation/record locks are not deleted. Filtered deletion uses intersecting `--older-than`, `--orphaned`, `--stopped`, and `--all` selectors rather than a prune command. Age and container absence are revalidated under lock before deletion updates its own activity timestamp.

`create` creates a new session and leaves its prepared container stopped; an existing session is never overwritten. `open` and `start` require retained session state and never create new sessions. Recreation preserves the session ID and stores. Missing-container recovery uses the exact recorded image, mount layout, source definition, and setup input; it does not choose newer configuration. Both `open` and `start` retain this recovery behavior. Already-running start/shell/exec do not resolve desired configuration. Starting a stopped container does: valid participating config is required and compatible runtime config is synchronized without changing recorded creation settings.

Clone creates a new session ID; relocate preserves it. Transfers copy declared environment stores and their managed-config manifests, excluding auth overlays, shared caches, leases, and container-layer data. Opaque symlinks are copied without traversal; special files are rejected. The external journal contains public endpoint identities, IDs, mode, phase, intended running state, and input fingerprints, not env/auth values or another creation record. Completion removes it; no permanent lineage is kept.

The source installation's data is not imported automatically. The separate migration tool remains pending. See [commands](commands.md) and [progress](../../dev/progress.md).
