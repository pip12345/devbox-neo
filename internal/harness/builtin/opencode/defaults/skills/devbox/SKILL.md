---
name: devbox
description: Use for tooling, networking, persistence, or Devbox questions inside this development container.
---

Read `/devbox/AGENTS.md` first, then `/devbox/docs/index.md` and the relevant guide or reference. These are Devbox-managed inputs, not editable session state.

`/workspace` is the host-mounted project. The host Docker daemon and `devbox-neo` CLI normally live outside the container. Do not run a Docker daemon or try to manage this container from inside itself.

Use `/devbox/network/env` or `/devbox/network/inspect.json` for network facts. Workspace files, declared harness state, managed auth, and shared caches persist separately. Container-layer installations do not survive recreation.
