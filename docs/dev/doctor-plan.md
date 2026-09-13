# Doctor — superseded proposal

## Status

Not planned. The selected workflow is the top-level environment `status --all` overview: saved sessions, missing-container state, configuration errors, drift reasons, and unmatched-container warnings. The proposal below is retained as historical design context, not an implementation task. Do not add a duplicate doctor command for these checks.

## Goal

Provide a focused, read-only command that explains common configuration/state problems and identifies containers needing recreation or image rebuilding.

## Proposed command

```text
devbox-neo doctor [--json]
```

## Checks

- Validate global/profile configuration and effective harness definitions, including invalid user overrides.
- Check managed auth/state paths for missing required inputs or incorrect file types. Do not read or expose credential contents.
- Report corrupt session records and missing recorded workspace directories.
- For recorded environments, use the existing resolver and fingerprint comparison to report pending container recreation or image rebuilding. Report invalid desired configuration separately from live container state.
- If Docker is unavailable, continue local checks and explicitly report that container checks could not run.

## Output

Each finding includes severity, target, problem, and a suggested action when available. Provide concise human output and equivalent structured JSON.

Errors produce a nonzero exit status. Warnings alone remain successful. An unavailable required check must not be presented as a clean pass.

## Implementation constraints

- Reuse existing validators, inventory, ownership checks, and drift comparison. Do not create competing validation or resolution rules.
- Keep the command read-only, including startup: do not initialize homes, seed files, repair state, reap leases, or mutate Docker resources.
- Do not expose env/auth values in text, JSON, or errors.
- No automatic fixes, plugin framework, persistent diagnostic cache, or recursive session-size scans.
- Ordinary recreation already rebuilds changed image inputs. Do not recommend a forced no-cache build merely because image inputs changed.

## Validation when implemented

- Cover clean state, invalid config/definitions, corrupt records, missing required paths, and unavailable Docker.
- Cover unchanged environments, container-input drift, image-input drift, and invalid desired configuration.
- Verify text/JSON output, exit statuses, redaction, and absence of filesystem/Docker mutations.
- Update relevant guide/reference/architecture docs and embedded guidance together.
- Run rewrite `make test` and `make check`, plus the parent repository's `make test`. Report live-Docker checks separately if unavailable.
