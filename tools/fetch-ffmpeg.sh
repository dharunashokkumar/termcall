#!/bin/sh
# Fetch the static ffmpeg for one target and put it where an embedded build
# expects it.
#
#   tools/fetch-ffmpeg.sh linux amd64
#   GOOS=linux GOARCH=amd64 go build -tags embedffmpeg ./cmd/tc
#
# The upstream assets are already gzipped, which is the form the embed reads,
# so this only has to pick the right one and rename it.
set -eu

# Pinned, not "latest". A release built today and rebuilt in a year should put
# the same ffmpeg inside the binary, and "latest" quietly breaks that.
VERSION="${FFMPEG_RELEASE:-b6.1.1}"
BASE="https://github.com/eugeneware/ffmpeg-static/releases/download/$VERSION"

goos="${1:-${GOOS:-}}"
goarch="${2:-${GOARCH:-}}"
[ -n "$goos" ] && [ -n "$goarch" ] || {
	echo "usage: $0 <goos> <goarch>" >&2
	exit 2
}

case "$goos/$goarch" in
	linux/amd64)   slug=linux-x64 ;;
	linux/arm64)   slug=linux-arm64 ;;
	darwin/amd64)  slug=darwin-x64 ;;
	darwin/arm64)  slug=darwin-arm64 ;;
	windows/amd64) slug=win32-x64 ;;
	*)
		echo "no static ffmpeg published for $goos/$goarch" >&2
		echo "build without -tags embedffmpeg; the client will ask for a system ffmpeg" >&2
		exit 1
		;;
esac

dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out="$dir/internal/video/ffmpeg.bin.gz"

echo "fetching ffmpeg $VERSION for $goos/$goarch"
curl -fsSL "$BASE/ffmpeg-$slug.gz" -o "$out"

# ffmpeg is GPL. It is executed as a separate program rather than linked, so
# termcall stays MIT, but the licence has to travel with the binary that
# carries it.
curl -fsSL "$BASE/$slug.LICENSE" -o "$dir/internal/video/FFMPEG-LICENSE.txt"

echo "wrote $out ($(wc -c < "$out") bytes)"
