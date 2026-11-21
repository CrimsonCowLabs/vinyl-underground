#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Pull the missing cover of every retagged album, in one go: optionally mount the share writable,
# back up what will change, run `retag.sh covers`, and unmount again (see ../README.md).
#
# Usage: run-covers.sh [OPTIONS] [QUERY...]
#        run-covers.sh --restore DIR [--dry-run]
#
# QUERY is a beets query, e.g. 'albumartist:Radiohead' or 'path:/music/Some/Folder'. None means
# every album beets retagged.
#
# What it does:
#   -n, --dry-run          say what it would do for each album, and write nothing; its list goes to
#                          $RETAG_STATE/no-cover-dry-run.tsv
#       --no-embed         only write cover.jpg into each folder; never touch the Files
#       --embed-size PX    shrink the embedded copy to PX pixels (default 600; 0 embeds it as it is)
#       --min-size PX      a cover is good from PX pixels wide (default 500)
#       --max-size PX      shrink a downloaded cover to PX pixels (default 1200)
#       --sources LIST     where to look, in order, comma-separated (default: all, in this order):
#                          local (an image in the folder, then the Files' art), coverart (Cover Art
#                          Archive by MusicBrainz ID), fanarttv, itunes, wikipedia
#       --local-only       the same as --sources local: no web requests at all
#
# Backups:
#   -b, --backup           back up each File before its cover is embedded, and list each cover.jpg
#                          written, in $RETAG_STATE/backups/YYYYmmdd-HHMMSS
#       --backup-dir DIR   the same, in DIR (outside the share); implies --backup
#       --restore DIR      undo the runs backed up in DIR: put each File back and remove each cover.jpg
#                          they wrote, leaving alone any changed since (takes only --dry-run and
#                          the "Where" options)
#
# Where:
#       --music DIR        the share's writable mount (default $MUSIC_RW, else /mnt/music-rw)
#       --state DIR        beets' state from the retag (default $RETAG_STATE; required)
#       --mount SHARE      mount SHARE (e.g. //NAS/music) writable at --music first, with sudo, and
#                          unmount it at the end
#       --credentials FILE the SMB credentials file for --mount
#       --smb-version V    the SMB version for --mount (default 1.0)
#       --keep-mount       leave the --mount mount up at the end
#
# Other:
#       --health DB        first save the health report's no-cover list from the app's database DB,
#                          to $RETAG_STATE/no-cover-before-YYYYmmdd-HHMMSS.tsv, to compare after a Full Scan
#       --build            rebuild the beets image first (needed after changing beets/)
#   -y, --yes              don't ask before writing to the share without a backup
#   -h, --help             show this help
set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
STAMP=$(date +%Y%m%d-%H%M%S)

usage() { sed -n '6,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-2}"; }
die() { echo "run-covers.sh: $*" >&2; exit 1; }
pixels() { [[ $2 =~ ^[0-9]+$ ]] || die "$1 takes a number of pixels, not '$2'"; }
sha256() { sha256sum < "$1" | cut -d' ' -f1; }

# Refuse a folder of ours (a backup) inside the share, before anything is written to it.
outside_share() {
  local dir share
  dir=$(realpath -m "$2") share=$(realpath -m "$music")
  case "$dir/" in "$share"/*) die "$1 must be outside the share: $2" ;; esac
}

music=${MUSIC_RW:-/mnt/music-rw}
state=${RETAG_STATE:-}
dry_run=0 embed=1 backup=0 backup_dir="" restore="" yes=0 build=0
embed_size="" min_size="" max_size="" sources=""
share="" credentials="" smb_version=1.0 keep_mount=0 health_db=""
query=()
run_only=""  # the first option given that only a run takes, not --restore

while (( $# )); do
  case "$1" in
    -n | --dry-run) dry_run=1 ;;
    --no-embed) run_only=${run_only:-$1}; embed=0 ;;
    --embed-size) run_only=${run_only:-$1}; pixels "$1" "${2:-}"; embed_size=$2; shift ;;
    --min-size) run_only=${run_only:-$1}; pixels "$1" "${2:-}"; min_size=$2; shift ;;
    --max-size) run_only=${run_only:-$1}; pixels "$1" "${2:-}"; max_size=$2; shift ;;
    --sources) run_only=${run_only:-$1}; sources=${2:?--sources takes a list}; shift ;;
    --local-only) run_only=${run_only:-$1}; sources=local ;;
    -b | --backup) run_only=${run_only:-$1}; backup=1 ;;
    --backup-dir) run_only=${run_only:-$1}; backup=1; backup_dir=${2:?--backup-dir takes a folder}; shift ;;
    --restore) restore=${2:?--restore takes a backup folder}; shift ;;
    --music) music=${2:?--music takes a folder}; shift ;;
    --state) run_only=${run_only:-$1}; state=${2:?--state takes a folder}; shift ;;
    --mount) share=${2:?--mount takes a share, e.g. //NAS/music}; shift ;;
    --credentials) credentials=${2:?--credentials takes a file}; shift ;;
    --smb-version) smb_version=${2:?--smb-version takes a version}; shift ;;
    --keep-mount) keep_mount=1 ;;
    --health) run_only=${run_only:-$1}; health_db=${2:?--health takes the app database}; shift ;;
    --build) run_only=${run_only:-$1}; build=1 ;;
    -y | --yes) run_only=${run_only:-$1}; yes=1 ;;
    -h | --help) usage 0 ;;
    --) shift; query+=("$@"); run_only=${run_only:-QUERY}; break ;;
    -*) die "unknown option $1 (see --help)" ;;
    *) query+=("$1"); run_only=${run_only:-QUERY} ;;
  esac
  shift
done

# The fetchart sources each name in --sources stands for, in beets' config.
fetchart_sources() {
  local name out=()
  IFS=, read -ra names <<< "$1"
  for name in "${names[@]}"; do
    case "$name" in
      local) out+=("filesystem" "embedded") ;;
      coverart) out+=("{coverart: release}" "{coverart: releasegroup}") ;;
      fanarttv | itunes | wikipedia) out+=("$name") ;;
      *) die "unknown source '$name': choose from local, coverart, fanarttv, itunes, wikipedia" ;;
    esac
  done
  (( ${#out[@]} )) || die "--sources is empty"
  local IFS=,
  echo "[${out[*]}]"
}

mounted=0
mount_share() {
  [[ -n $share ]] || return 0
  if mountpoint -q "$music"; then
    echo "$music is already mounted; using it as it is"
    return 0
  fi
  [[ -n $credentials ]] || die "--mount needs --credentials FILE"
  echo "Mounting $share writable at $music (sudo)"
  # mount reads the file as root, and it is usually readable by root only (e.g. /etc/samba-creds).
  sudo test -r "$credentials" || die "can't read the credentials file $credentials, even with sudo"
  sudo mkdir -p "$music"
  sudo mount -t cifs "$share" "$music" \
    -o "rw,vers=$smb_version,credentials=$credentials,uid=$(id -u),gid=$(id -g),file_mode=0644,dir_mode=0755,iocharset=utf8,noserverino"
  mounted=1
}

unmount_share() {
  if (( mounted && ! keep_mount )); then
    echo "Unmounting $music"
    sudo umount "$music" || echo "run-covers.sh: couldn't unmount $music; unmount it by hand" >&2
  fi
}

# A path from a manifest, which must stay inside the share.
safe_path() {
  case "$1" in
    "" | /* | ../* | */../* | .. | */..) return 1 ;;
  esac
}

do_restore() {
  local dir=$1 manifest="$1/manifest.tsv" kind path digest now
  local put=0 removed=0 kept=0 missing=0 done=0
  [[ -f $manifest ]] || die "no manifest.tsv in $dir"
  [[ -d $music ]] || die "the share isn't at $music (use --music, or --mount)"
  (( dry_run )) && echo "Dry run: nothing will be changed"
  # Each path once, however many runs went into the backup: what the first run wrote.
  while IFS=$'\t' read -r kind path digest; do
    safe_path "$path" || die "manifest.tsv names a path outside the share: $path"
    # A cover listed but never written (a run stopped mid-write), or removed since: nothing to do.
    if [[ $kind == cover && ! -e "$music/$path" ]]; then continue; fi
    now=$( [[ -f "$music/$path" ]] && sha256 "$music/$path" || true)
    case "$kind" in
      file)
        if [[ ! -f "$dir/files/$path" ]]; then
          echo "missing   $path: no copy in $dir/files; put it back by hand" >&2
          missing=$((missing + 1))
        elif [[ $now == "$(sha256 "$dir/files/$path")" ]]; then
          done=$((done + 1))
        elif [[ $now == "$digest" ]]; then
          echo "put back  $path"
          (( dry_run )) || cp -p "$dir/files/$path" "$music/$path"
          put=$((put + 1))
        else
          echo "keep      $path (changed since the run; its copy is in $dir/files)"
          kept=$((kept + 1))
        fi ;;
      cover)
        if [[ $now == "$digest" ]]; then
          echo "remove    $path"
          (( dry_run )) || rm -- "$music/$path"
          removed=$((removed + 1))
        else
          echo "keep      $path (changed since the run)"
          kept=$((kept + 1))
        fi ;;
      *) die "manifest.tsv has an unknown line: $kind" ;;
    esac
  done < <(awk -F'\t' '!seen[$1 FS $2]++' "$manifest")
  echo "$put Files put back, $removed covers removed, $kept kept as changed since, $done already as they were"
  (( missing )) && { echo "$missing Files had no copy in the backup" >&2; return 1; }
  echo "Then run a Full Scan in the app."
}

if [[ -n $restore ]]; then
  [[ -z $run_only ]] || die "--restore doesn't take $run_only"
  outside_share --restore "$restore"
  [[ -z $share ]] || mount_share
  trap unmount_share EXIT
  do_restore "$restore"
  exit 0
fi

[[ -n $state ]] || die "set RETAG_STATE, or --state, to the folder the retag used"
[[ -f $state/library.db ]] || die "no beets library in $state: run the retag first, with the same RETAG_STATE"
(( dry_run && backup )) && die "--dry-run writes nothing, so it has nothing to back up"
(( ! embed )) && [[ -n $embed_size ]] && die "--embed-size has no effect with --no-embed"

if (( build )); then
  "$HERE/retag.sh" build
fi

if [[ -n $health_db ]]; then
  before="$state/no-cover-before-$STAMP.tsv"
  python3 "$HERE/../health.py" "$health_db" --list no-cover > "$before"
  echo "Albums with no cover before: $(($(wc -l < "$before") - 1)), listed in $before"
fi

mount_share
trap unmount_share EXIT
[[ -d $music ]] || die "the share isn't at $music (use --music, or --mount)"

# Settings for this run, over beets/config.yaml.
fetchart_yaml=() covers_yaml=()
[[ -n $min_size ]] && fetchart_yaml+=("minwidth: $min_size")
[[ -n $max_size ]] && fetchart_yaml+=("maxwidth: $max_size")
[[ -n $sources ]] && fetchart_yaml+=("sources: $(fetchart_sources "$sources")")
[[ -n $embed_size ]] && covers_yaml+=("embed_maxwidth: $embed_size")
{
  echo "# Written by run-covers.sh for its last run; safe to delete."
  # Only sections with settings in them: an empty one would blank beets' own.
  if (( ${#fetchart_yaml[@]} )); then echo "fetchart:"; printf '  %s\n' "${fetchart_yaml[@]}"; fi
  if (( ${#covers_yaml[@]} )); then echo "covers:"; printf '  %s\n' "${covers_yaml[@]}"; fi
} > "$state/run-covers.yaml"

opts=()
(( dry_run )) && opts+=(--pretend)
(( embed )) || opts+=(--no-embed)
if (( backup )); then
  backup_dir=${backup_dir:-$state/backups/$STAMP}
  outside_share --backup-dir "$backup_dir"
  mkdir -p "$backup_dir"
  # beets' library records which cover each album has, so keep a copy of it too: the one from
  # before the first run into this folder.
  [[ -e $backup_dir/library.db ]] || cp -p "$state/library.db" "$backup_dir/library.db"
  export RETAG_BACKUP=$backup_dir
  opts+=(--backup /backup)
fi

if (( ! dry_run && ! backup && ! yes )); then
  [[ -t 0 ]] || die "this writes to the share without a backup: add --backup, or --yes to go ahead"
  what="every retagged album"
  (( ${#query[@]} )) && what="the albums matching: ${query[*]}"
  read -rp "Write covers for $what into $music, with no backup? [y/N] " answer
  [[ $answer == [yY]* ]] || die "stopped; nothing was written"
fi

export MUSIC_RW=$music RETAG_STATE=$state RETAG_CONFIG=/state/run-covers.yaml
"$HERE/retag.sh" covers "${opts[@]}" "${query[@]}"

if (( backup )); then
  echo "Backup in $backup_dir: undo this run with run-covers.sh --restore $backup_dir"
fi
if (( ! dry_run )); then
  echo "Next: unmount the writable mount, if it's still up, and run a Full Scan in the app."
fi
