# Alternative: explicit environments and generic config directories

Status: alternative proposal only. This does not replace `config-directory-proposal.md`, change the runtime contract, or approve implementation.

## Model

Use three concepts:

- **Config directory:** reusable settings, Dockerfile, scripts, and native harness files.
- **Environment:** a workspace, an explicit name, and an ordered list of config directories.
- **Config UI:** inspect environments, manage their source lists, and edit source configuration.

There are no profile/project-specific config types, automatic profile/project selection, or names assembled from config components. Config files have neither a `name` nor an `inherit` field. The environment owns its name, and every explicitly selected source participates.

This is an alternative to the existing profile/project interface, not an additional mode layered on top of it.

## Commands

```sh
# Create a reusable config directory.
devbox-neo config init ./dev/base

# Create a named environment using configs in the supplied order.
devbox-neo create . --name myapp \
  --config ./dev/base \
  --config ./dev/local

# Show environments associated with this workspace.
devbox-neo config .

# Open the configuration UI for one environment.
devbox-neo config myapp

# Use the same environment name for lifecycle commands.
devbox-neo open myapp
devbox-neo shell myapp
devbox-neo status myapp
devbox-neo recreate myapp
```

### Initialize a config

`config init <directory>` creates configuration at the supplied location and offers the harness/artifact initialization workflow. It does not create an environment, register a profile, or change another environment's source list. Initialization preserves existing files rather than replacing them.

Directories are ordinary filesystem sources. They need no global registry or special storage location.

### Create an environment

`create <workspace> --name NAME --config DIR...` requires an explicit environment name and at least one config directory. Repeated `--config` arguments establish source order.

The workspace and config directories are separate inputs. The workspace is mounted into the container; each config directory supplies configuration and build inputs.

Resolve relative config paths against the invoking host working directory and save absolute references. Do not copy the sources or persist a flattened settings snapshot in their place.

Creation saves the workspace, name, and source order. It refuses an already-used environment name; it does not overwrite another environment. Subsequent commands use the saved selection without repeating creation flags.

There is no implicit `.devbox/` discovery, default profile, `--profile`, or `--no-default-config` mode in this alternative. A directory named `.devbox` can still be passed explicitly; its location gives it no special semantics. Built-in runtime defaults and installation-level settings remain separate from source selection and must not inject undisclosed config directories.

## Names and targeting

Recommended targeting contract:

- Environment names are unique within the selected Devbox home.
- A bare name such as `myapp` identifies an environment.
- `.`, `..`, an explicit relative path such as `./myapp`, or an absolute path identifies a workspace.
- Several differently named environments can use the same workspace.

For `config`, a folder target always opens the environment overview for that folder. A name target opens that environment directly. Do not reinterpret a missing environment name as a filesystem path.

The name is the environment's primary user-facing selector, not an alias or a concatenation of source names. Docker resource naming and ownership remain internal concerns; user-facing names do not replace ownership checks.

## Config directories

A directory can contain:

```text
config.json
Dockerfile
Dockerfile.dockerignore
setup.sh
before-open.sh
pi/
opencode/
tools/
  ...Docker build inputs...
```

Use one schema and one artifact contract for every directory. Add only the settings and artifacts required by that source.

Example `config.json`:

```json
{
  "harness": "pi",
  "base_image": "ubuntu:24.04",
  "ports": ["3000:3000"]
}
```

There is no config-level identity field. There is also no inheritance cutoff: all sources supplied by the environment's list participate, in that order. To exclude a source, remove it from the list.

## Composition

Retain the agreed settings/artifact composition model without the profile/project roles:

| Input | Rule |
|---|---|
| Scalar settings | Later explicit values replace earlier ones |
| Most list settings | Append in source order, following existing field rules |
| Environment assignments | Later assignment for the same variable wins |
| Shell argv | Later value replaces the complete argv |
| Configured harness arguments | Contribute only from sources explicitly naming the selected harness |
| Dockerfiles | Build sequentially, each extending the preceding image |
| `setup.sh` | Run sequentially during container creation/recreation |
| `before-open.sh` | Run sequentially before harness launch through `open` |
| Harness files | Overlay by relative path; later files win |

Missing settings or artifacts contribute nothing. They do not reset preceding contributions. This alternative does not introduce list-removal syntax or recursive config includes.

### Image customization

Resolve one upstream `base_image` from the merged settings, using Devbox's supported default when absent. Devbox prepares the development user and runtime before running user Dockerfiles.

Each contributing Dockerfile extends `DEVBOX_BASE`:

```dockerfile
ARG DEVBOX_BASE
FROM ${DEVBOX_BASE}

RUN sudo apt-get update \
    && sudo apt-get install -y ripgrep
```

For the first Dockerfile, `DEVBOX_BASE` is the prepared image. For subsequent Dockerfiles, it is the preceding build's result. Each Dockerfile keeps its own directory as its build context and uses its own ignore rules. Devbox installs the selected harness and validates the runtime contract afterward.

Keep the proposed build arguments: `DEVBOX_BASE`, `DEVBOX_USER`, `DEVBOX_USER_HOME`, `DEVBOX_WORKSPACE`, `DEVBOX_UID`, and `DEVBOX_GID`. Devbox owns user/permission scaffolding. Preserve user PATH additions and account for managed mounts that can hide image-installed files.

Tool installation remains image-cached. Identical effective image inputs should reuse results across environments; changing only a container setting must not independently reinstall tools. Earlier image changes invalidate dependent downstream cache. Caching is not a reproducibility guarantee for unpinned network inputs.

### Lifecycle scripts

- `setup.sh` runs for each newly created/recreated container with the workspace mounted.
- `before-open.sh` runs before each harness launch through `open`.

Run scripts in source order as the development user, with the workspace as the working directory and sudo available. Scripts are separate processes, not a shared shell. A failure stops the chain and fails the operation. Completed effects, including workspace writes, are not automatically rolled back.

Keep native harness configuration and existing managed-file/shared-JSON synchronization contracts.

## Configuration UI

### Workspace overview

`devbox-neo config .` shows the environments for the current workspace, including their names and selected config sources. Selecting an environment opens its configuration screen.

An empty overview should explain how to initialize a config and show the creation syntax. It must not silently create an environment or select some unrelated environment.

### Environment configuration

`devbox-neo config myapp` opens the selected environment directly:

```text
myapp — /work/myapp

Config sources:
  1. /work/myapp/dev/base
  2. /work/myapp/dev/local
```

The screen supports:

- Adding, removing, or reordering config sources for this environment.
- Editing a selected source's settings and artifacts.
- Inspecting the combined configuration, source provenance, and ordered build/script inputs.

The UI must distinguish these two kinds of edits:

1. **Change this environment's source list:** affects only which configs this environment uses.
2. **Edit a source directory:** changes the shared source, affecting other environments that reference it.

Show the actual source path and warn when other saved environments use it. Do not create hidden environment-private copies or write merged values back into an arbitrary source.

## Persistence and application

The environment's saved source list is authoritative for desired configuration. Reopening or recreating does not rediscover sources from the current directory, defaults, or unrelated environments.

- Editing source files changes desired configuration.
- Saving a different source order or set changes desired configuration for that environment.
- Missing or invalid required sources produce an actionable error rather than a fallback.
- `status` compares desired inputs with the applied environment.
- Recreation applies image/container changes while retaining saved session state and environment identity.
- Existing runtime synchronization rules govern compatible harness configuration updates.

Editing configuration does not silently recreate the container or claim unapplied settings are active. Keep desired sources distinct from the recorded applied inputs used for drift reporting and recovery.

Do not persist expanded secrets as part of a merged config snapshot. Config files remain the source of truth for sensitive environment references, subject to the existing recovery safeguards.

## Tradeoff

The model is smaller because it drops automatic profile/project discovery and their special selection rules. Every creation explicitly supplies its environment name and config directories once. Reuse comes from pointing multiple environments at the same directories.

The common path is still discoverable through `config init`, the workspace overview, and concrete creation hints. Do not reintroduce implicit profiles, extra naming modes, or a config registry to hide the explicit model.

## Implementation boundaries

This is a replacement design, not a compatibility layer. It requires changes to source selection, session records, identity/target parsing, configuration management, and their tests/docs—not just new flags.

Reuse existing configuration parsing/editing, source-root locks, artifact capture, managed-file synchronization, and lifecycle machinery where their contracts fit. Replace role-specific source/name parsing rather than adding parallel implementations.

Before implementation, define:

- Environment-name validation and collisions with command syntax.
- Copy/move destination naming and treatment of workspace-local versus external source paths.
- Safe validation and locking for saved source-list edits.
- Supported image bases, user/UID conflicts, and build-boundary/finalization checks.

No migration, aliases, old-format readers, or implementation work are authorized by this document.
