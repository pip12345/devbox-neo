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
| `state/locks/sessions/*.operation.lock` | Environment mutation lock |
| `state/locks/sessions/*.record.lock` | Short record read/write lock |
| `.build-*` | Temporary generated image context |

Docker creation uses a private `0600` env file in the OS temporary directory, removed when the create command returns. Its contents and path are not persisted in session records.

Locks remain outside removable session directories. Records are atomically replaced with restrictive permissions; corrupt state is not treated as absence.

Containers carry installation, ownership-version, session, workspace, and slot labels. Images carry installation ownership only. Final image tags are `devbox-rewrite/session:<session-id>`. Names are lookup keys, never proof of ownership.

Recreation preserves the session ID and stores. Missing-container recovery uses the exact recorded image, mount layout, source definition, and setup input; it does not choose newer configuration. Existing-container start/shell/exec do not require those old source inputs just to access the container.

The source installation's data is not imported automatically. Session management commands and the separate migration tool are still pending; see [progress](../../dev/progress.md).
