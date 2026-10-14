#!/usr/bin/env bash
# Builds the static FFmpeg libraries gocue links against: FFmpeg's own
# demuxers, decoders and parsers plus the filters the scan uses, without any
# external library. A full FFmpeg build makes gocue load dozens of libraries
# (video encoders, fonts, TLS) on every start; this one adds none.
#
# Usage: scripts/build-ffmpeg.sh <install-prefix>
# Needs a C compiler, make, curl, xz and, on x86, nasm.
set -euo pipefail

version=9.0.1
sha256=cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635

prefix=${1:?usage: $0 <install-prefix>}
mkdir -p "$prefix"
prefix=$(cd "$prefix" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -fsSL "https://ffmpeg.org/releases/ffmpeg-$version.tar.xz" -o "$work/ffmpeg.tar.xz"
if command -v sha256sum >/dev/null; then
  echo "$sha256  $work/ffmpeg.tar.xz" | sha256sum -c -
else
  echo "$sha256  $work/ffmpeg.tar.xz" | shasum -a 256 -c -
fi
tar -xJf "$work/ffmpeg.tar.xz" -C "$work" --strip-components=1

cd "$work"
./configure \
  --prefix="$prefix" \
  --disable-shared --enable-static --enable-pic \
  --disable-programs --disable-doc --disable-network --disable-autodetect \
  --disable-everything --disable-avdevice --disable-swscale \
  --enable-protocol=file \
  --enable-demuxers --enable-decoders --enable-parsers \
  --enable-filter=abuffer,abuffersink,aresample,ebur128,pan
make -j"$(getconf _NPROCESSORS_ONLN)"
make install
