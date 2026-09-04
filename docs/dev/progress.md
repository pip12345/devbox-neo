# Implementation progress

The approved scope is [rewrite-plan.md](rewrite-plan.md). The migration utility remains a separate delivery described in [migration-plan.md](migration-plan.md).

## Phase 0 — complete

- Independent Go module; Makefile targets for format, unit/race tests, build, and opt-in Docker integration.
- Linux-only implementation and toolchain installer.
- Development binary `bin/devbox-neo`, default home `~/.devbox-neo`, and separate Docker resource names/ownership labels.
- `--home` > `DEVBOX_HOME` > development default; the conventional `~/.devbox` and its descendants are rejected.
- Cobra smoke tests, process-boundary Docker recorder, and a stateful Docker fake.

## Phase 1 — candidate implemented, real-Docker gate outstanding

Implemented and covered by unit/fake-backed tests:

- Parsed embedded Pi definition, generic stores/auth, and ordinary/`json-keys` managed configuration.
- Pure layer/artifact selection, explicit-profile isolation, and standalone project inheritance exclusion.
- Immutable resolved inputs, keyed env/definition fingerprints, and actual image ID in the applied container fingerprint.
- One strict session record, atomic writes, stable installation identity, external operation/record locks, Linux PID/start/boot leases, and last-attached-command cleanup.
- Installation-owned image builds, session-specific final tags, and exact container ownership/instance checks.
- Create/open/reopen, non-blocking creation drift, stopped-only config synchronization, running-container deferral, and explicit recreation with durable state preserved.
- No-cache forced rebuilds; explicit recreation builds a missing image, whereas recorded recovery refuses it.
- Per-container setup and every-open entrypoint scripts. Setup inputs belong to creation fingerprints because synchronization cannot claim a changed setup script already ran.
- Config-independent existing-container start/shell/exec and recorded recovery for the currently supported inputs. Definition-sourced env values are not stored in session JSON.
- Failure propagation, failed-commit cleanup, cancellation cleanup, concurrent-lease safety, and preserved foreground exit/signal status.
- A custom fixture already exercises the same generic engine; this does not complete the full Phase 2 schema/acceptance gate.

The real-Docker test uses Pi's non-interactive version command to exercise launch without provider credentials and verifies durable state preservation across recreation. Provider login, an interactive terminal session, and conversation continuation still need host-side acceptance coverage.

## Validation

- `make check`: unit tests, race tests, and build pass.
- `go vet ./...`: passes.
- `make install-go`: downloaded and checksum-verified the pinned Linux amd64 toolchain successfully; arm64 has a verified official checksum but was not executed here.
- Integration-tagged tests compile.
- `make test-integration`: **blocked** because this workspace has no Docker CLI/socket. The target fails explicitly rather than reporting a skipped gate as a pass.
- The parent repository's `make test` remains a separate check; it does not validate the rewrite.

Run the gate on a Linux host as a non-root user with Docker access:

```sh
cd rewrite
make test-integration
```

It uses temporary homes and rewrite-only ownership labels, not the existing installation or the persistent `~/.devbox-neo` home.

## Remaining work

Do not mark Phase 1 complete or advance to dashboards until the real-Docker gate passes.

- Phase 2: OpenCode definition, full built-in/custom mapping and auth acceptance, and schema freeze.
- Phase 3: host env substitution/reference recovery and multiline env transport, full env/mount/port/raw-arg/IDE settings, normal/full Dockerfile contexts, artifact-only project participation, runtime docs/assets, complete provenance, and config `--show`.
- Phase 4: complete target resolution, container inventory/status/logs/delete, bulk recreation, network facts/commands, and expanded lifecycle/crash testing.
- Phase 5: session views, reset/prune/delete, clone/relocate, and transfer interruption recovery.
- Phase 6: resource commands, dashboards, create/init guidance and seeding, doctor, and complete documentation.
- Phase 7: release hardening, performance/secret audits, and remaining acceptance tests.
- Separate migration utility: not implemented.

Definition env uses a private temporary env file, keeping values out of process arguments and the host Docker client's environment. Multiline values are explicitly rejected before Docker work; the wider env feature remains pending.

Pending creation options are explicitly rejected instead of being silently ignored. Initial-creation failures can leave uncommitted host state when it cannot be safely classified; retries refuse to adopt it. This is not a production-ready cutover build.
