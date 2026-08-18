#!/bin/sh
# cria installer: fetches the latest release binary for this platform and puts
# it in ~/.local/bin (override with CRIA_BIN_DIR). curl sets no quarantine
# attribute, so the macOS binary runs without any Gatekeeper step.
#
#   curl -fsSL https://raw.githubusercontent.com/acdtrx/cria/main/install.sh | sh
set -eu

repo="acdtrx/cria"
bin_dir="${CRIA_BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) echo "cria has no build for $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
arm64 | aarch64) arch=arm64 ;;
x86_64) arch=amd64 ;;
*) echo "cria has no build for $(uname -m)" >&2; exit 1 ;;
esac
if [ "$os" = darwin ] && [ "$arch" != arm64 ]; then
    echo "cria ships for Apple silicon Macs only" >&2
    exit 1
fi

asset="cria_${os}_${arch}.tar.gz"
url="https://github.com/${repo}/releases/latest/download/${asset}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading ${url}"
curl -fsSL "$url" -o "${tmp}/${asset}"
tar -xzf "${tmp}/${asset}" -C "$tmp"
mkdir -p "$bin_dir"
install -m 0755 "${tmp}/cria" "${bin_dir}/cria"

echo "installed $("${bin_dir}/cria" --version) to ${bin_dir}/cria"
case ":$PATH:" in
*":${bin_dir}:"*) ;;
*) echo "note: ${bin_dir} is not on your PATH" ;;
esac
