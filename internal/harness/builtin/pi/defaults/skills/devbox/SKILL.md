---
name: devbox
description: Use for tooling, networking, persistence, or Devbox questions inside this development container.
---

Read `/devbox/AGENTS.md` first, then `/devbox/docs/index.md` and the relevant guide or reference. These are Devbox-managed inputs, not editable session state.

`/workspace` is the host-mounted project. The host Docker daemon and `devbox-neo` CLI normally live outside the container. Do not run a Docker daemon or try to manage this container from inside itself.

Use `/devbox/ssh/config` and its included connection files to discover user-supplied SSH aliases. Run `ssh`/`scp` with `-F /devbox/ssh/config`. Never handle interactive SSH login, search for host credentials, or bypass a missing shared connection. Ask the user to run `devbox-neo ssh <environment> <destination>` in a host terminal; use the exact environment name from the network facts. See `/devbox/docs/guides/ssh.md` for host-master mode, jump hosts, and lifetime rules. Access does not authorize unrelated remote changes.

Use `/devbox/network/env` or `/devbox/network/inspect.json` for network facts. Workspace files, declared harness state, managed auth, and shared caches persist separately. Container-layer installations do not survive recreation.
