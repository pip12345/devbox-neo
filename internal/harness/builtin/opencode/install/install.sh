#!/bin/bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt-get install -y --no-install-recommends nodejs
sudo rm -rf /var/lib/apt/lists/*
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
case $(uname -m) in
    x86_64) bun_archive=bun-linux-x64-baseline; target=opencode-linux-x64-baseline ;;
    aarch64) bun_archive=bun-linux-aarch64; target=opencode-linux-arm64 ;;
    *) echo 'OpenCode source build supports Linux x86_64 and aarch64' >&2; exit 1 ;;
esac
curl -fsSL "https://github.com/oven-sh/bun/releases/download/bun-v1.4.2/$bun_archive.zip" -o "$build/bun.zip"
unzip -q "$build/bun.zip" -d "$build"
export PATH="$build/$bun_archive:$PATH"
git clone --filter=blob:none --no-checkout --single-branch --branch v2 https://github.com/anomalyco/opencode.git "$build/source"
cd "$build/source"
git checkout --detach 4c33a253aa89ec0fa4faaa5d5b4aefef7d1a3963
HUSKY=0 bun install --frozen-lockfile
OPENCODE_CHANNEL=dev OPENCODE_VERSION=0.0.0-devbox-4c33a253aa89 bun run packages/cli/script/build.ts --target="$target" --skip-install

bin=$HOME/.opencode/bin
mkdir -p "$bin"
install -m 0755 "packages/cli/dist/${target/opencode/cli}/bin/opencode" "$bin/opencode-native"
install -m 0755 "$here/opencode-auth.sh" "$bin/opencode-auth.sh"
cat >"$bin/opencode" <<'LAUNCHER'
#!/bin/bash
bin=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
exec bash "$bin/opencode-auth.sh" "$bin/opencode-native" /home/devuser/.local/share/devbox-opencode-auth /home/devuser/.local/share/opencode "$@"
LAUNCHER
chmod 0755 "$bin/opencode"
