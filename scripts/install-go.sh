#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
version=1.24.2
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
archive="go${version}.${os}-${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://go.dev/dl/$archive" -o "$tmp/$archive"
curl -fsSL "https://go.dev/dl/$archive.sha256" -o "$tmp/checksum"
printf '%s  %s\n' "$(tr -d '\n' < "$tmp/checksum")" "$tmp/$archive" | sha256sum -c -
tar -C "$tmp" -xzf "$tmp/$archive"
mkdir -p "$root/.tools"
rm -rf "$root/.tools/go"
mv "$tmp/go" "$root/.tools/go"
"$root/.tools/go/bin/go" version
