#!/usr/bin/env bash
# Generates example/media/playlist.pls from the audio files in $MUSIC_DIR.
#
# The paths written into the playlist are CONTAINER paths (/music/...), because
# the station reads the file from inside the container where $MUSIC_DIR is
# mounted at /music. Nothing about your host layout reaches the playlist, and
# the playlist itself is git-ignored.
#
# Each entry is wrapped in an annotate: URI. That is Liquidsoap's metadata
# protocol: the fields ride along with the request and are merged with the
# file's own tags when the track starts. Deliberately no TitleN or LengthN
# entries, so the real ID3 tags are what listeners see.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
example_dir="$(dirname "$here")"
out="$example_dir/media/playlist.pls"

# .env is the only place your real music path lives. Read the two keys we need
# rather than sourcing the file: compose allows unquoted values with spaces,
# which a shell would try to execute.
env_file="$example_dir/.env"

read_env() {
  local key="$1"
  [[ -f "$env_file" ]] || return 0
  sed -n "s/^[[:space:]]*${key}=//p" "$env_file" | tail -n 1 |
    sed -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'$/\1/"
}

MUSIC_DIR="${MUSIC_DIR:-$(read_env MUSIC_DIR)}"
STATION_SHOW="${STATION_SHOW:-$(read_env STATION_SHOW)}"

: "${MUSIC_DIR:?set MUSIC_DIR in example/.env (copy .env.example first)}"
if [[ ! -d "$MUSIC_DIR" ]]; then
  echo "MUSIC_DIR is not a directory: $MUSIC_DIR" >&2
  exit 1
fi

show="${STATION_SHOW:-Early 40s}"
[[ -n "$show" ]] || show="Early 40s"
slots=(A B C)

mkdir -p "$example_dir/media"
entries="$(mktemp)"
trap 'rm -f "$entries"' EXIT

n=0
while IFS= read -r f; do
  [[ -n "$f" ]] || continue
  n=$((n + 1))
  slot="${slots[$(((n - 1) % ${#slots[@]}))]}"
  printf 'File%d=annotate:station_show="%s",rotation_slot="%s",playlist_position="%d":/music/%s\n' \
    "$n" "${show//\"/\\\"}" "$slot" "$n" "$(basename "$f")" >>"$entries"
done < <(find "$MUSIC_DIR" -maxdepth 1 -type f \
  \( -iname '*.mp3' -o -iname '*.flac' -o -iname '*.ogg' \
  -o -iname '*.opus' -o -iname '*.m4a' -o -iname '*.wav' \) | LC_ALL=C sort)

if [[ "$n" -eq 0 ]]; then
  echo "no audio files found in $MUSIC_DIR" >&2
  exit 1
fi

{
  echo "[playlist]"
  echo "NumberOfEntries=$n"
  cat "$entries"
  echo "Version=2"
} >"$out"

echo "wrote $out ($n entries)"
