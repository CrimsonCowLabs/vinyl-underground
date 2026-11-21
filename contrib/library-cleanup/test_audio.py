# SPDX-License-Identifier: GPL-3.0-only
import io
import struct
import unittest

from audio import audio_data

# Two MPEG-1 Layer III frame headers (128 kbps, 44.1 kHz) with a little payload each.
FRAMES = b"\xff\xfb\x90\x64" + b"\x11" * 60 + b"\xff\xfb\x90\x64" + b"\x22" * 60
OTHER_FRAMES = b"\xff\xfb\x90\x64" + b"\x33" * 60 + b"\xff\xfb\x90\x64" + b"\x44" * 60


def syncsafe(n: int) -> bytes:
    return bytes([(n >> 21) & 0x7F, (n >> 14) & 0x7F, (n >> 7) & 0x7F, n & 0x7F])


def id3v2(title: str, padding: int = 0, version: int = 3, footer: bool = False) -> bytes:
    text = b"\x00" + title.encode("latin-1")
    frame = b"TIT2" + struct.pack(">I", len(text)) + b"\x00\x00" + text
    body = frame + b"\x00" * padding
    flags = 0x10 if footer else 0
    tag = b"ID3" + bytes([version, 0, flags]) + syncsafe(len(body)) + body
    if footer:
        tag += b"3DI" + bytes([version, 0, flags]) + syncsafe(len(body))
    return tag


def id3v1(title: str) -> bytes:
    return b"TAG" + title.encode("latin-1").ljust(30, b"\x00") + b"\x00" * 94 + b"\x0c"


def apev2(value: str, header: bool = True) -> bytes:
    item = struct.pack("<II", len(value), 0) + b"Title\x00" + value.encode()
    size = len(item) + 32  # items plus footer
    def block(is_header: bool) -> bytes:
        flags = (0x80000000 if header else 0) | (0x20000000 if is_header else 0)
        return b"APETAGEX" + struct.pack("<IIII", 2000, size, 1, flags) + b"\x00" * 8
    return (block(True) if header else b"") + item + block(False)


def mp3(audio: bytes, head: bytes = b"", tail: bytes = b"") -> io.BytesIO:
    return io.BytesIO(head + audio + tail)


class Mp3(unittest.TestCase):
    def test_files_that_differ_only_in_their_id3_tags_hold_the_same_audio(self) -> None:
        original = mp3(FRAMES, id3v2("Car Alarm"), id3v1("Car Alarm"))
        copy = mp3(FRAMES, id3v2("Car Alarm (copy)", padding=300), id3v1("Car Alarm 1"))

        self.assertEqual(audio_data(original, "mp3"), FRAMES)
        self.assertEqual(audio_data(copy, "mp3"), FRAMES)

    def test_leaves_out_ape_tags_before_or_after_an_id3v1_tag(self) -> None:
        # Seen in the Library: ID3v2, audio, APEv2 (from a gain tool), then ID3v1.
        with_header = mp3(FRAMES, id3v2("A"), apev2("Song") + id3v1("Song"))
        footer_only = mp3(FRAMES, b"", apev2("Song 1", header=False))
        two_tags = mp3(FRAMES, b"", apev2("x") + id3v1("Song") + apev2("y"))

        for f in (with_header, footer_only, two_tags):
            self.assertEqual(audio_data(f, "mp3"), FRAMES)

    def test_leaves_out_chained_and_appended_id3v2_tags_and_lyrics3(self) -> None:
        # Seen in the Library: an ID3v2.3 tag followed by an ID3v2.4 tag.
        chained = mp3(FRAMES, id3v2("A", version=3) + id3v2("A", version=4, padding=50))
        appended = mp3(FRAMES, b"", id3v2("A", version=4, footer=True))
        lyrics = b"LYRICSBEGIN" + b"IND00002" + b"11" + b"000021" + b"LYRICS200"
        with_lyrics = mp3(FRAMES, b"", lyrics + id3v1("A"))

        for f in (chained, appended, with_lyrics):
            self.assertEqual(audio_data(f, "mp3"), FRAMES)

    def test_a_file_that_is_all_tag_has_no_audio_to_compare(self) -> None:
        # e.g. a truncated download whose ID3v2 tag claims more bytes than the File holds.
        self.assertIsNone(audio_data(io.BytesIO(id3v2("A", padding=10)[:-5]), "mp3"))
        self.assertIsNone(audio_data(mp3(b"", id3v2("A"), id3v1("A")), "mp3"))

    def test_keeps_audio_that_only_looks_like_the_end_of_a_tag(self) -> None:
        # Frame data can end in "3DI", "APETAGEX" or "LYRICS200" by chance; with no tag
        # header where the size points, those bytes are audio and are compared.
        for fake in (
            b"3DI\x04\x00\x10" + syncsafe(40),
            b"APETAGEX" + struct.pack("<IIII", 2000, 60, 1, 0x80000000) + b"\x00" * 8,
            b"000030LYRICS200",
        ):
            data = FRAMES + fake
            self.assertEqual(audio_data(mp3(data), "mp3"), data)

    def test_different_frames_are_different_audio(self) -> None:
        self.assertNotEqual(audio_data(mp3(FRAMES, id3v2("A")), "mp3"), audio_data(mp3(OTHER_FRAMES, id3v2("A")), "mp3"))


def atom(kind: bytes, payload: bytes, wide: bool = False) -> bytes:
    if wide:
        return struct.pack(">I", 1) + kind + struct.pack(">Q", len(payload) + 16) + payload
    return struct.pack(">I", len(payload) + 8) + kind + payload


SAMPLES = b"\x21\x10\x05" * 40


def m4a(title: str, samples: bytes = SAMPLES, mdat_first: bool = False, wide: bool = False) -> io.BytesIO:
    ftyp = atom(b"ftyp", b"M4A \x00\x00\x00\x00M4A mp42isom")
    moov = atom(b"moov", atom(b"udta", atom(b"meta", b"\x00" * 4 + atom(b"\xa9nam", title.encode()))))
    mdat = atom(b"mdat", samples, wide)
    body = mdat + moov if mdat_first else moov + atom(b"free", b"\x00" * 16) + mdat
    return io.BytesIO(ftyp + body)


class M4a(unittest.TestCase):
    def test_the_audio_is_the_media_data_whatever_the_metadata_around_it(self) -> None:
        for f in (m4a("Song"), m4a("Song 1", mdat_first=True), m4a("Song (copy)", wide=True)):
            self.assertEqual(audio_data(f, "m4a"), SAMPLES)

    def test_different_media_data_is_different_audio(self) -> None:
        self.assertNotEqual(audio_data(m4a("Song"), "m4a"), audio_data(m4a("Song", samples=b"\x00" * 120), "m4a"))

    def test_a_file_without_media_data_has_no_audio_to_compare(self) -> None:
        self.assertIsNone(audio_data(io.BytesIO(atom(b"ftyp", b"M4A ") + atom(b"moov", b"")), "m4a"))
        self.assertIsNone(audio_data(io.BytesIO(b"not an mp4 file at all"), "m4a"))
        self.assertIsNone(audio_data(m4a("Song", samples=b""), "m4a"))


def chunk(kind: bytes, payload: bytes, order: str = ">") -> bytes:
    pad = b"\x00" if len(payload) % 2 else b""
    return kind + struct.pack(order + "I", len(payload)) + payload + pad


COMM = b"\x00\x02" + b"\x00\x00\x00\x28" + b"\x00\x10" + b"\x40\x0e\xac\x44" + b"\x00" * 6
PCM = b"\x01\x02\x03\x04" * 20


def aiff(title: str, comm: bytes = COMM, tags_after: bool = False) -> io.BytesIO:
    tags = chunk(b"NAME", title.encode()) + chunk(b"ID3 ", id3v2(title))
    sound = chunk(b"COMM", comm) + chunk(b"SSND", PCM)
    body = b"AIFF" + (sound + tags if tags_after else tags + sound)
    return io.BytesIO(b"FORM" + struct.pack(">I", len(body)) + body)


def wav(title: str, fmt: bytes = b"\x01\x00\x02\x00\x44\xac\x00\x00\x10\xb1\x02\x00\x04\x00\x10\x00") -> io.BytesIO:
    info = chunk(b"LIST", b"INFO" + chunk(b"INAM", title.encode() + b"\x00", "<"), "<")
    body = b"WAVE" + chunk(b"fmt ", fmt, "<") + info + chunk(b"data", PCM, "<")
    return io.BytesIO(b"RIFF" + struct.pack("<I", len(body)) + body)


class AiffAndWav(unittest.TestCase):
    def test_files_that_differ_only_in_their_tag_chunks_hold_the_same_audio(self) -> None:
        self.assertEqual(audio_data(aiff("Song"), "aif"), audio_data(aiff("Song 1 (odd)", tags_after=True), "aiff"))
        self.assertEqual(audio_data(wav("Song"), "wav"), audio_data(wav("Song 1"), "wav"))
        self.assertIn(PCM, audio_data(aiff("Song"), "aif") or b"")

    def test_the_same_samples_in_a_different_format_are_different_audio(self) -> None:
        mono = b"\x00\x01" + COMM[2:]
        self.assertNotEqual(audio_data(aiff("Song"), "aif"), audio_data(aiff("Song", comm=mono), "aif"))
        self.assertNotEqual(audio_data(wav("Song"), "wav"), audio_data(wav("Song", fmt=b"\x01\x00\x01\x00" + b"\x00" * 12), "wav"))


class Unsupported(unittest.TestCase):
    def test_formats_it_cannot_parse_have_no_audio_to_compare(self) -> None:
        self.assertIsNone(audio_data(io.BytesIO(b"OggS" + b"\x00" * 60), "ogg"))


if __name__ == "__main__":
    unittest.main()
