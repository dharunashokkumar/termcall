#!/bin/sh
# termcall installer. Fetches the single binary for this machine.
#
#   curl -fsSL https://__HOST__/install.sh | sh
#
# Everything the client needs is inside that one file, ffmpeg included, so
# there is nothing else to fetch and nothing to build.
set -eu

REPO="dharunashokkumar/termcall"

fail() {
	echo "install: $1" >&2
	exit 1
}

os=$(uname -s 2>/dev/null || echo unknown)
arch=$(uname -m 2>/dev/null || echo unknown)

case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "termcall has no build for $os. Windows has its own: install.ps1" ;;
esac

case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "termcall has no build for $arch" ;;
esac

# Somewhere on PATH that does not need root. A user-writable directory is
# preferred over sudo: an installer that asks for a password to drop one
# static binary is asking for more trust than it needs.
for dir in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
	if [ -d "$dir" ] && [ -w "$dir" ]; then
		target="$dir"
		break
	fi
done
if [ -z "${target:-}" ]; then
	target="$HOME/.local/bin"
	mkdir -p "$target" || fail "could not create $target"
fi

url="https://github.com/$REPO/releases/latest/download/tc_${os}_${arch}"
tmp=$(mktemp) || fail "could not make a temporary file"
trap 'rm -f "$tmp"' EXIT

echo "fetching termcall for $os/$arch…"
if command -v curl >/dev/null 2>&1; then
	curl -fsSL "$url" -o "$tmp" || fail "download failed: $url"
elif command -v wget >/dev/null 2>&1; then
	wget -qO "$tmp" "$url" || fail "download failed: $url"
else
	fail "neither curl nor wget is installed"
fi

chmod +x "$tmp"
mv "$tmp" "$target/tc" || fail "could not write $target/tc"
trap - EXIT

echo "installed $target/tc"
case ":$PATH:" in
	*":$target:"*) echo "run: tc" ;;
	*) echo "add $target to your PATH, then run: tc" ;;
esac
