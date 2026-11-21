#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Retag the Library in place from MusicBrainz with beets, in Docker (see ../README.md).
#
# Usage: retag.sh build              build the image
#        retag.sh test               run the plugins' tests in the image
#        retag.sh import PATHS.txt   retag the paths retag.py listed, asking about each album
#        retag.sh review             go through the albums skipped so far, asking about every one
#        retag.sh covers [QUERY]     give each retagged album (or those QUERY matches) a cover.jpg, embedded
#
# MUSIC_RW   the share's temporary writable mount (default /mnt/music-rw); beets sees it as /music
# RETAG_STATE  a folder outside the share for beets' library, resume state and import log (required)
# RETAG_QUIET=1  (import only) apply strong matches and skip the rest without asking: only for a test on a scratch copy
set -euo pipefail

IMAGE=vinyl-underground-beets
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
MUSIC_RW=${MUSIC_RW:-/mnt/music-rw}

usage() { sed -n '4,13p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 2; }

beet() {
  : "${RETAG_STATE:?set RETAG_STATE to a folder outside the share}"
  mkdir -p "$RETAG_STATE"
  local music state
  music=$(realpath "$MUSIC_RW")
  state=$(realpath "$RETAG_STATE")
  [[ -d $music ]] || { echo "MUSIC_RW is not a folder: $MUSIC_RW" >&2; exit 1; }
  case "$state/" in "$music"/*) echo "RETAG_STATE must be outside the share: $RETAG_STATE" >&2; exit 1 ;; esac
  # beets reads the owner's answers from stdin, so keep it open, as a terminal when there is one.
  local tty=(-i)
  [[ -t 0 && -t 1 ]] && tty=(-it)
  docker run --rm "${tty[@]}" --user "$(id -u):$(id -g)" \
    -v "$music:/music" -v "$state:/state" "$IMAGE" "$@"
}

quiet=()
[[ ${RETAG_QUIET:-} == 1 ]] && quiet=(-c /etc/beets/quiet.yaml)

case "${1:-}" in
  build)
    docker build -t "$IMAGE" "$HERE" ;;
  test)
    docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -e PYTHONPATH=/etc/beets -w /etc/beets \
      --entrypoint python "$IMAGE" -m unittest -v test_lowconfidence test_fingerprints test_keepgenre test_covers ;;
  import)
    [[ $# -eq 2 ]] || usage
    paths=()
    while IFS= read -r line; do
      [[ -z $line ]] && continue
      case "$line" in /* | ../* | */../* | .. | */..) echo "not a path inside the share: $line" >&2; exit 1 ;; esac
      paths+=("/music/$line")
    done < "$2"
    (( ${#paths[@]} )) || { echo "no paths in $2" >&2; exit 1; }
    # Nothing is approved in quiet mode, so it only runs on a scratch copy, never on a mounted share.
    if (( ${#quiet[@]} )) && mountpoint -q "$MUSIC_RW"; then
      echo "RETAG_QUIET=1 is only for a scratch copy, and $MUSIC_RW is a mount" >&2; exit 1
    fi
    beet "${quiet[@]}" import "${paths[@]}" ;;
  review)
    [[ $# -eq 1 ]] || usage
    : "${RETAG_STATE:?set RETAG_STATE to a folder outside the share}"
    grep -q '^skip ' "$RETAG_STATE/import.log" 2>/dev/null || { echo "no skipped albums in $RETAG_STATE/import.log" >&2; exit 0; }
    beet -c /etc/beets/review.yaml import --from-logfile /state/import.log ;;
  covers)
    shift
    beet covers "$@" ;;
  *)
    usage ;;
esac
