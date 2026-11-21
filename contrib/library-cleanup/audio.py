# SPDX-License-Identifier: GPL-3.0-only
"""Find the audio in a File, leaving out its tags and embedded art.

Two Files hold the same audio when these bytes are identical, even if their tags differ.
Works on any seekable binary file object and reads only the tag headers until asked
for the audio itself, so the size of the audio is cheap to learn over a slow share.
"""
from __future__ import annotations

import hashlib
import os
from typing import BinaryIO, Iterator


def _syncsafe(b: bytes) -> int:
    return (b[0] << 21) | (b[1] << 14) | (b[2] << 7) | b[3]


def _mp3_range(f: BinaryIO, size: int) -> tuple[int, int]:
    start = 0
    while True:
        f.seek(start)
        head = f.read(10)
        if len(head) < 10 or head[:3] != b"ID3":
            break
        start += 10 + _syncsafe(head[6:10]) + (10 if head[5] & 0x10 else 0)
    end = size
    while True:
        tag = _trailing_tag(f, start, end)
        if not tag or end - tag < start:
            return start, end
        end -= tag


def _at(f: BinaryIO, pos: int, magic: bytes) -> bool:
    if pos < 0:
        return False
    f.seek(pos)
    return f.read(len(magic)) == magic


def _trailing_tag(f: BinaryIO, start: int, end: int) -> int:
    """The size of the tag that ends at `end`, or 0 if none does.

    A tag is only recognised when its header sits where its footer says, so audio that
    happens to end in a tag's marker is still compared."""
    if end - start < 32:
        return 0
    f.seek(max(start, end - 128))
    tail = f.read(end - max(start, end - 128))
    if len(tail) == 128 and tail[:3] == b"TAG":  # ID3v1
        return 128
    if tail[-32:-24] == b"APETAGEX":  # APEv2 footer; the size counts the items and footer, not the header
        size = int.from_bytes(tail[-20:-16], "little")
        has_header = int.from_bytes(tail[-12:-8], "little") & 0x80000000
        tag = size + (32 if has_header else 0)
        version = int.from_bytes(tail[-24:-20], "little")
        if version in (1000, 2000) and size >= 32 and (not has_header or _at(f, end - tag, b"APETAGEX")):
            return tag
    if tail[-9:] == b"LYRICS200":  # Lyrics3v2: a 6-digit size, then the marker
        digits = tail[-15:-9]
        tag = int(digits) + 15 if digits.isdigit() else 0
        if tag and _at(f, end - tag, b"LYRICSBEGIN"):
            return tag
    if tail[-10:-7] == b"3DI" and all(b < 0x80 for b in tail[-4:]):  # an ID3v2 tag appended at the end, with its footer
        tag = 20 + _syncsafe(tail[-4:])
        if _at(f, end - tag, b"ID3" + tail[-7:-6]):
            return tag
    return 0


def _mp4_ranges(f: BinaryIO, size: int) -> list[tuple[int, int]]:
    # Top-level atoms: the samples live in "mdat"; tags and art live in "moov", which also
    # moves when a tagger rewrites it, so only the media data is compared.
    ranges, pos = [], 0
    while pos + 8 <= size:
        f.seek(pos)
        head = f.read(16)
        length, kind = int.from_bytes(head[:4], "big"), head[4:8]
        header = 8
        if length == 1 and len(head) == 16:
            length, header = int.from_bytes(head[8:16], "big"), 16
        elif length == 0:
            length = size - pos
        if length < header or pos + length > size:
            return []
        if kind == b"mdat":
            ranges.append((pos + header, pos + length))
        pos += length
    return ranges


# Chunked formats: the chunks that make up the sound (its format and its samples). Every
# other chunk (NAME, ID3, LIST, ...) is metadata.
_IFF = {b"FORM": ("big", {b"COMM", b"SSND"}), b"RIFF": ("little", {b"fmt ", b"data"})}


def _iff_ranges(f: BinaryIO, size: int) -> list[tuple[int, int]]:
    f.seek(0)
    if (spec := _IFF.get(f.read(4))) is None:
        return []
    order, sound = spec
    ranges, pos = [], 12
    while pos + 8 <= size:
        f.seek(pos)
        head = f.read(8)
        end = pos + 8 + int.from_bytes(head[4:8], order)
        if end > size:
            return []
        if head[:4] in sound:
            ranges.append((pos, end))  # with its header, so a chunk's kind and length count too
        pos = end + (end - pos) % 2  # chunks are padded to an even length
    return ranges


def audio_ranges(f: BinaryIO, ext: str) -> list[tuple[int, int]] | None:
    """The byte ranges holding a File's audio, or None if the format isn't supported or the File can't be parsed."""
    size = f.seek(0, 2)
    ext = ext.lower().lstrip(".")
    if ext == "mp3":
        start, end = _mp3_range(f, size)
        return [(start, end)] if start < end else None
    if ext in ("m4a", "mp4", "m4b"):
        ranges = _mp4_ranges(f, size)
        return ranges if sum(end - start for start, end in ranges) > 0 else None
    if ext in ("aif", "aiff", "aifc", "wav"):
        return _iff_ranges(f, size) or None
    return None


def _read(f: BinaryIO, ranges: list[tuple[int, int]], chunk: int = 1 << 20) -> Iterator[bytes]:
    for start, end in ranges:
        f.seek(start)
        while start < end:
            block = f.read(min(chunk, end - start))
            if not block:
                raise OSError(f"File ended early, at byte {start}")
            start += len(block)
            yield block


def audio_data(f: BinaryIO, ext: str) -> bytes | None:
    """The bytes of a File's audio, or None if the format isn't supported."""
    ranges = audio_ranges(f, ext)
    return None if ranges is None else b"".join(_read(f, ranges))


def _ext(path: str) -> str:
    return os.path.splitext(path)[1]


def audio_size(path: str) -> int | None:
    """How many bytes of audio the File at `path` holds. Reads only its tag headers."""
    with open(path, "rb") as f:
        ranges = audio_ranges(f, _ext(path))
    return None if ranges is None else sum(end - start for start, end in ranges)


def audio_digest(path: str) -> str | None:
    """A SHA-256 of the File's audio: equal digests mean identical audio."""
    with open(path, "rb") as f:
        ranges = audio_ranges(f, _ext(path))
        if ranges is None:
            return None
        h = hashlib.sha256()
        for block in _read(f, ranges):
            h.update(block)
    return h.hexdigest()


def same_audio(a: str, b: str) -> bool:
    """Whether the Files at `a` and `b` hold identical audio, whatever their tags."""
    size = audio_size(a)
    return size is not None and size == audio_size(b) and audio_digest(a) == audio_digest(b)
