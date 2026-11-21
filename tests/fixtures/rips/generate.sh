#!/bin/sh
# Regenerates the Rip fixtures: one second of silence per format, tagged with the
# SOURCE / SOURCEURL contract from issue #28, plus one untagged File. Needs ffmpeg
# and mutagen, e.g. from the repository root:
#   docker run --rm -v "$PWD":/w -w /w alpine:3 sh -c \
#     'apk add -q ffmpeg py3-mutagen && sh tests/fixtures/rips/generate.sh'
set -eu
cd "$(dirname "$0")"
src="YouTube"
url="https://www.youtube.com/watch?v=dQw4w9WgXcQ"

gen() { # gen <title> <out> <ffmpeg codec options...>
  title=$1 out=$2
  shift 2
  ffmpeg -loglevel error -y -f lavfi -i anullsrc=r=48000:cl=mono -t 1 -map_metadata -1 \
    -metadata title="$title" -metadata artist="Rip Artist" -metadata album="Rip Album" "$@" "$out"
}

gen "Ripped Opus" ripped.opus -c:a libopus -b:a 8k
gen "Ripped M4A" ripped.m4a -c:a aac -b:a 16k
gen "Ripped MP3" ripped.mp3 -ar 22050 -c:a libmp3lame -b:a 8k -id3v2_version 3
gen "Not Ripped" untagged.opus -c:a libopus -b:a 8k

python3 - "$src" "$url" <<'PY'
import sys
from mutagen.oggopus import OggOpus
from mutagen.mp4 import MP4, MP4FreeForm
from mutagen.id3 import ID3, TXXX

src, url = sys.argv[1], sys.argv[2]

o = OggOpus("ripped.opus")
o["SOURCE"] = src
o["SOURCEURL"] = url
o.save()

m = MP4("ripped.m4a")
m["----:com.apple.iTunes:SOURCE"] = [MP4FreeForm(src.encode())]
m["----:com.apple.iTunes:SOURCEURL"] = [MP4FreeForm(url.encode())]
m.save()

i = ID3("ripped.mp3")
i.add(TXXX(encoding=3, desc="SOURCE", text=[src]))
i.add(TXXX(encoding=3, desc="SOURCEURL", text=[url]))
i.save(v2_version=3)
PY
