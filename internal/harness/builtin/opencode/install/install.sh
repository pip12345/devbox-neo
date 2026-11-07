#!/bin/bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt-get install -y --no-install-recommends nodejs
sudo rm -rf /var/lib/apt/lists/*
curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path

# Native updates replace ~/.opencode/bin/opencode. Keep the launcher outside
# that directory so updating OpenCode cannot bypass shared-auth synchronization.
bin=$HOME/.opencode/devbox/bin
mkdir -p -- "$bin"
install -m 0755 "$here/opencode-auth.sh" "$bin/opencode-auth.sh"
cat >"$bin/opencode" <<'LAUNCHER'
#!/bin/bash
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
exec bash "$root/devbox/bin/opencode-auth.sh" "$root/bin/opencode" /home/devuser/.local/share/devbox-opencode-auth /home/devuser/.local/share/opencode "$@"
LAUNCHER
chmod 0755 "$bin/opencode"
