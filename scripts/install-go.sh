#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
version=1.24.2
if [[ "$(uname -s)" != Linux ]]; then
  echo 'Devbox supports Linux only.' >&2
  exit 1
fi
os=linux
arch="$(uname -m)"
# Pins come from https://go.dev/dl/?mode=json&include=all. Update them with the version.
case "$arch" in
  x86_64) arch=amd64; checksum=68097bd680839cbc9d464a0edce4f7c333975e27a90246890e9f1078c7e702ad ;;
  aarch64|arm64) arch=arm64; checksum=756274ea4b68fa5535eb9fe2559889287d725a8da63c6aae4d5f23778c229f4b ;;
  *) echo 'The pinned installer supports Linux amd64 and arm64.' >&2; exit 1 ;;
esac
archive="go${version}.${os}-${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://go.dev/dl/$archive" -o "$tmp/$archive"
printf '%s  %s\n' "$checksum" "$tmp/$archive" | sha256sum -c -
tar -C "$tmp" -xzf "$tmp/$archive"
mkdir -p "$root/.tools"
rm -rf "$root/.tools/go"
mv "$tmp/go" "$root/.tools/go"
"$root/.tools/go/bin/go" version
