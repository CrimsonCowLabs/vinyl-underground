# beets for retagging the Library in place from MusicBrainz (see ../README.md). Build with `retag.sh build`.
FROM python:3.12-slim-bookworm@sha256:935c3c5b12357a7e10caa9c1e6ca5692344d97cf229832ef584be2cec9fc03df

# Chromaprint for the chroma plugin's audio fingerprints, and ffmpeg, which decodes the audio for it
# (pyacoustid prefers the Chromaprint library and audioread, and audioread needs ffmpeg).
RUN apt-get update \
    && apt-get install -y --no-install-recommends libchromaprint-tools=1.5.1-2+b1 ffmpeg=7:5.1.9-0+deb12u1 \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --root-user-action=ignore -r /tmp/requirements.txt

COPY config.yaml review.yaml quiet.yaml test_*.py /etc/beets/
COPY beetsplug/ /etc/beets/beetsplug/

# Settings come from /etc/beets; the library, state and import log go to /state (a volume outside the share).
ENV BEETSDIR=/etc/beets HOME=/state PYTHONDONTWRITEBYTECODE=1
WORKDIR /state
ENTRYPOINT ["beet"]
