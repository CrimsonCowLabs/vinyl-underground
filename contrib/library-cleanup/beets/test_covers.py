# SPDX-License-Identifier: GPL-3.0-only
"""Tests for the covers plugin. They need beets, so run them in the image: retag.sh test."""
import hashlib
import io
import os
import shutil
import subprocess
import tempfile
import unittest

import mediafile
from beets import config
from beets.library import Item, Library
from beets.util import get_temp_filename
from beets.util.artresizer import ArtResizer
from beetsplug import fetchart
from PIL import Image

from beetsplug.covers import CoversPlugin


def image(path: str, width: int, height: int | None = None, fmt: str = "JPEG", colour: str = "red") -> str:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    Image.new("RGB", (width, height or width), colour).save(path, fmt)
    return path


def size(data: bytes) -> tuple[str, tuple[int, int]]:
    with Image.open(io.BytesIO(data)) as im:
        return im.format, im.size


def read(path: str) -> bytes:
    with open(path, "rb") as fh:
        return fh.read()


class FakeRemote(fetchart.RemoteArtSource):
    """Stands in for the Cover Art Archive and the other online sources: `found` maps an album's
    name to the image the lookup returns."""
    NAME = "Fake remote"
    ID = "fakeremote"
    found: dict[str, str] = {}
    asked: list[str] = []

    def get(self, album, plugin, paths):
        FakeRemote.asked.append(album.album)
        if album.album in self.found:
            yield self._candidate(url=self.found[album.album], match=fetchart.MetadataMatch.EXACT)

    def fetch_image(self, candidate, plugin) -> None:
        temp = get_temp_filename(__name__, suffix=os.path.splitext(candidate.url)[1])
        shutil.copy(candidate.url, temp)
        candidate.path = os.fsencode(temp)


class Covers(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        folder = tempfile.TemporaryDirectory()
        cls.addClassCleanup(folder.cleanup)
        cls.mp3 = os.path.join(folder.name, "silence.mp3")
        subprocess.run(["ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono",
                        "-t", "1", "-q:a", "9", cls.mp3], check=True)

    def setUp(self) -> None:
        config.clear()
        config.read(user=False, defaults=True)
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.music = os.path.join(folder.name, "music")
        self.images = os.path.join(folder.name, "online")
        os.makedirs(self.music)
        config["directory"] = self.music
        config["create_backup_before_migrations"] = False
        config["ignore"] = [".*", "*~", "_Quarantine", "trashbox"]
        config["fetchart"].set({"auto": False, "minwidth": 500, "maxwidth": 1200, "cautious": True,
                                "enforce_ratio": "10%", "sources": ["filesystem", "embedded", "fakeremote"]})
        config["covers"].set({"embed_maxwidth": 600, "report": os.path.join(folder.name, "no-cover.tsv")})
        self.report = os.path.join(folder.name, "no-cover.tsv")

        fetchart.ART_SOURCES.add(FakeRemote)
        self.addCleanup(fetchart.ART_SOURCES.discard, FakeRemote)
        FakeRemote.found, FakeRemote.asked = {}, []
        self.plugin = CoversPlugin()
        self.plugin.fetchart = fetchart.FetchArtPlugin()
        self.lib = Library(os.path.join(folder.name, "library.db"))

    def album(self, folder: str, tracks: int = 2, name: str = "The Bends", embedded: bytes | None = None):
        """An album of `tracks` Files in `folder` (relative to the share), as the retag left it."""
        items = []
        for n in range(1, tracks + 1):
            path = os.path.join(self.music, folder, f"{n:02} Track.mp3")
            os.makedirs(os.path.dirname(path), exist_ok=True)
            shutil.copy(self.mp3, path)
            if embedded:
                mf = mediafile.MediaFile(path)
                mf.images = [mediafile.Image(embedded, type=mediafile.ImageType.front)]
                mf.save()
            items.append(Item(path=os.fsencode(path), title=f"Track {n}", album=name, albumartist="Radiohead",
                              artist="Radiohead", track=n))
        return self.lib.add_album(items)

    def online(self, album: str, width: int, colour: str = "blue") -> None:
        FakeRemote.found[album] = image(os.path.join(self.images, f"{album}.jpg"), width, colour=colour)

    def embedded(self, folder: str) -> list[tuple[str, tuple[int, int]] | None]:
        files = sorted(f for f in os.listdir(os.path.join(self.music, folder)) if f.endswith(".mp3"))
        art = [mediafile.MediaFile(os.path.join(self.music, folder, f)).art for f in files]
        return [size(a) if a else None for a in art]

    def test_resizes_covers_itself_rather_than_through_a_web_proxy(self) -> None:
        # Without Pillow, fetchart would send every cover's URL to a resizing service on the web.
        self.assertTrue(ArtResizer.shared.local)

    def test_an_album_with_no_cover_gets_one_from_online_in_its_folder_and_files(self) -> None:
        album = self.album("Radiohead/The Bends")
        self.online("The Bends", 3000)

        result = self.plugin.cover(album)

        self.assertEqual(result.status, "found")
        cover = os.path.join(self.music, "Radiohead/The Bends/cover.jpg")
        self.assertEqual(size(read(cover)), ("JPEG", (1200, 1200)))
        self.assertEqual(self.embedded("Radiohead/The Bends"), [("JPEG", (600, 600))] * 2)
        self.assertEqual(os.fsdecode(self.lib.get_album(album.id).artpath), cover)

    def test_keeps_a_cover_jpg_already_in_the_folder_as_it_is(self) -> None:
        album = self.album("Radiohead/The Bends")
        cover = image(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"), 3000, colour="green")
        before = read(cover)
        self.online("The Bends", 1200)

        result = self.plugin.cover(album)

        self.assertEqual(result.status, "kept")
        self.assertEqual(read(cover), before)
        self.assertEqual(FakeRemote.asked, [])
        self.assertEqual(self.embedded("Radiohead/The Bends"), [("JPEG", (600, 600))] * 2)

    def test_keeps_a_good_image_in_the_folder_and_leaves_it_in_place(self) -> None:
        album = self.album("Radiohead/The Bends")
        folder_png = image(os.path.join(self.music, "Radiohead/The Bends/Folder.png"), 800, fmt="PNG")
        before = read(folder_png)
        self.online("The Bends", 1200)

        result = self.plugin.cover(album)

        self.assertEqual(result, ("kept", "filesystem"))
        self.assertEqual(read(folder_png), before)
        self.assertEqual(size(read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"))), ("JPEG", (800, 800)))
        self.assertEqual(FakeRemote.asked, [])

    def test_keeps_good_embedded_art_and_files_that_have_art_keep_their_own(self) -> None:
        album = self.album("Radiohead/The Bends", embedded=read(image(os.path.join(self.images, "e.png"), 700, fmt="PNG")))
        self.online("The Bends", 1200)

        result = self.plugin.cover(album)

        self.assertEqual(result, ("kept", "embedded"))
        self.assertEqual(size(read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"))), ("JPEG", (700, 700)))
        self.assertEqual(self.embedded("Radiohead/The Bends"), [("PNG", (700, 700))] * 2)
        self.assertEqual(FakeRemote.asked, [])

    def test_looks_online_when_the_cover_it_has_is_too_small(self) -> None:
        album = self.album("Radiohead/The Bends")
        small = image(os.path.join(self.music, "Radiohead/The Bends/Folder.jpg"), 200)
        self.online("The Bends", 1000)

        result = self.plugin.cover(album)

        self.assertEqual(result, ("found", "fakeremote"))
        self.assertEqual(size(read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"))), ("JPEG", (1000, 1000)))
        self.assertTrue(os.path.exists(small))

    def test_keeps_a_small_cover_when_nothing_better_is_found(self) -> None:
        album = self.album("Radiohead/The Bends")
        image(os.path.join(self.music, "Radiohead/The Bends/Folder.jpg"), 200)

        result = self.plugin.cover(album)

        self.assertEqual(result, ("small", "filesystem"))
        self.assertEqual(size(read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"))), ("JPEG", (200, 200)))
        self.assertEqual(self.embedded("Radiohead/The Bends"), [("JPEG", (200, 200))] * 2)

    def test_writes_nothing_when_nothing_is_found(self) -> None:
        album = self.album("Radiohead/The Bends")

        result = self.plugin.cover(album)

        self.assertEqual(result.status, "none")
        self.assertEqual(sorted(os.listdir(os.path.join(self.music, "Radiohead/The Bends"))),
                         ["01 Track.mp3", "02 Track.mp3"])
        self.assertEqual(self.embedded("Radiohead/The Bends"), [None, None])

    def test_puts_a_cover_in_every_folder_of_an_album_on_several_discs(self) -> None:
        cd1 = self.album("Radiohead/OK Computer/CD1", name="OK Computer")
        cd2_path = os.path.join(self.music, "Radiohead/OK Computer/CD2/01 Track.mp3")
        os.makedirs(os.path.dirname(cd2_path))
        shutil.copy(self.mp3, cd2_path)
        self.lib.add(Item(path=os.fsencode(cd2_path), title="Lull", album="OK Computer", albumartist="Radiohead",
                          disc=2, album_id=cd1.id))
        self.online("OK Computer", 1200)

        self.assertEqual(self.plugin.cover(cd1).status, "found")

        for disc in ("CD1", "CD2"):
            self.assertTrue(os.path.exists(os.path.join(self.music, "Radiohead/OK Computer", disc, "cover.jpg")))
        self.assertEqual(self.embedded("Radiohead/OK Computer/CD2"), [("JPEG", (600, 600))])

    def test_never_touches_quarantine_the_recycle_bin_or_ignored_folders(self) -> None:
        for folder in ("_Quarantine/Radiohead/The Bends", "trashbox/Old", "Mixes/Summer"):
            self.album(folder)
        open(os.path.join(self.music, "Mixes/.ndignore"), "w").close()
        self.online("The Bends", 1200)

        for album in self.lib.albums():
            self.assertEqual(self.plugin.cover(album).status, "left out")

        for folder in ("_Quarantine/Radiohead/The Bends", "trashbox/Old", "Mixes/Summer"):
            self.assertNotIn("cover.jpg", os.listdir(os.path.join(self.music, folder)))
            self.assertEqual(self.embedded(folder), [None, None])

    def test_lists_the_albums_left_without_a_good_cover(self) -> None:
        self.album("Radiohead/The Bends")
        self.album("Radiohead/Pablo Honey", name="Pablo Honey")
        image(os.path.join(self.music, "Radiohead/Pablo Honey/front.jpg"), 300)
        self.album("Radiohead/OK Computer", name="OK Computer")
        self.online("OK Computer", 1200)

        self.plugin.cover_all(self.lib.albums())

        with open(self.report, encoding="utf-8") as fh:
            self.assertEqual(fh.read(), "status\talbum artist\talbum\tfolders\n"
                                        "small\tRadiohead\tPablo Honey\tRadiohead/Pablo Honey\n"
                                        "none\tRadiohead\tThe Bends\tRadiohead/The Bends\n")

    def test_keeps_a_small_cover_jpg_already_there_but_lists_it(self) -> None:
        album = self.album("Radiohead/The Bends")
        cover = image(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"), 200)
        before = read(cover)
        self.online("The Bends", 1200)

        self.assertEqual(self.plugin.cover(album), ("small", "cover.jpg"))
        self.assertEqual(read(cover), before)

    def test_an_album_whose_files_could_not_all_be_given_the_cover_is_listed_as_an_error(self) -> None:
        album = self.album("Radiohead/The Bends")
        with open(os.path.join(self.music, "Radiohead/The Bends/01 Track.mp3"), "wb") as fh:
            fh.write(b"not audio")
        self.online("The Bends", 1200)

        result = self.plugin.cover(album)

        self.assertEqual(result.status, "error")
        self.assertIn("01 Track.mp3", result.source)

    def test_lists_each_album_as_it_goes_so_a_stopped_run_still_has_its_list(self) -> None:
        self.album("Radiohead/The Bends")
        self.album("Radiohead/Pablo Honey", name="Pablo Honey")

        def stop_after_the_first(albums):
            yield next(iter(albums))
            raise KeyboardInterrupt

        with self.assertRaises(KeyboardInterrupt):
            self.plugin.cover_all(stop_after_the_first(self.lib.albums()))

        with open(self.report, encoding="utf-8") as fh:
            self.assertEqual(fh.read(), "status\talbum artist\talbum\tfolders\n"
                                        "none\tRadiohead\tPablo Honey\tRadiohead/Pablo Honey\n")

    def test_a_second_run_keeps_what_the_first_wrote(self) -> None:
        album = self.album("Radiohead/The Bends")
        self.online("The Bends", 1200)
        self.plugin.cover(album)
        cover = read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg"))
        self.online("The Bends", 1200, colour="yellow")

        self.assertEqual(self.plugin.cover(album), ("kept", "cover.jpg"))
        self.assertEqual(read(os.path.join(self.music, "Radiohead/The Bends/cover.jpg")), cover)

    # The options run-covers.sh passes: a dry run, a backup, and covers in folders only.

    def test_a_dry_run_says_what_it_would_do_and_writes_nothing(self) -> None:
        album = self.album("Radiohead/The Bends")
        kept = self.album("Radiohead/Pablo Honey", name="Pablo Honey")
        image(os.path.join(self.music, "Radiohead/Pablo Honey/Folder.png"), 800, fmt="PNG")
        self.online("The Bends", 3000)
        config["covers"]["pretend"] = True
        before = {f: read(os.path.join(self.music, "Radiohead/The Bends", f)) for f in ("01 Track.mp3", "02 Track.mp3")}

        self.assertEqual(self.plugin.cover(album).status, "found")
        self.assertEqual(self.plugin.cover(kept).status, "kept")

        self.assertEqual(sorted(os.listdir(os.path.join(self.music, "Radiohead/The Bends"))), sorted(before))
        self.assertEqual({f: read(os.path.join(self.music, "Radiohead/The Bends", f)) for f in before}, before)
        self.assertFalse(os.path.exists(os.path.join(self.music, "Radiohead/Pablo Honey/cover.jpg")))
        self.assertEqual(self.embedded("Radiohead/Pablo Honey"), [None, None])
        self.assertIsNone(self.lib.get_album(album.id).artpath)

    def test_a_backup_keeps_each_file_as_it_was_before_the_cover_went_in(self) -> None:
        backup = os.path.join(os.path.dirname(self.music), "backup")
        album = self.album("Radiohead/The Bends")
        original = read(os.path.join(self.music, "Radiohead/The Bends/01 Track.mp3"))
        self.online("The Bends", 1200)
        config["covers"]["backup"] = backup

        self.plugin.cover(album)

        self.assertEqual(read(os.path.join(backup, "files/Radiohead/The Bends/01 Track.mp3")), original)
        cover = os.path.join(self.music, "Radiohead/The Bends/cover.jpg")
        digest = hashlib.sha256(read(cover)).hexdigest()
        # Each File's checksum after the cover went in, so a restore can tell it hasn't changed since.
        after = [hashlib.sha256(read(os.path.join(self.music, "Radiohead/The Bends", f))).hexdigest()
                 for f in ("01 Track.mp3", "02 Track.mp3")]
        with open(os.path.join(backup, "manifest.tsv"), encoding="utf-8") as fh:
            self.assertEqual(fh.read().splitlines(), [
                f"cover\tRadiohead/The Bends/cover.jpg\t{digest}",
                f"file\tRadiohead/The Bends/01 Track.mp3\t{after[0]}",
                f"file\tRadiohead/The Bends/02 Track.mp3\t{after[1]}",
            ])

    def test_a_backup_leaves_out_files_it_does_not_change(self) -> None:
        backup = os.path.join(os.path.dirname(self.music), "backup")
        art = read(image(os.path.join(self.images, "art.jpg"), 800))
        album = self.album("Radiohead/The Bends", embedded=art)
        config["covers"]["backup"] = backup

        self.plugin.cover(album)

        self.assertFalse(os.path.exists(os.path.join(backup, "files")))
        with open(os.path.join(backup, "manifest.tsv"), encoding="utf-8") as fh:
            self.assertEqual([l.split("\t")[0] for l in fh.read().splitlines()], ["cover"])

    def test_with_embedding_off_only_the_folder_gets_the_cover(self) -> None:
        album = self.album("Radiohead/The Bends")
        self.online("The Bends", 1200)
        config["covers"]["embed"] = False

        self.assertEqual(self.plugin.cover(album).status, "found")

        self.assertTrue(os.path.exists(os.path.join(self.music, "Radiohead/The Bends/cover.jpg")))
        self.assertEqual(self.embedded("Radiohead/The Bends"), [None, None])

    def test_the_command_takes_the_options_for_a_dry_run_a_backup_and_no_embedding(self) -> None:
        cmd = self.plugin.commands()[0]
        opts, args = cmd.parser.parse_args(["--pretend", "--backup", "/backup", "--no-embed", "album:Bends"])

        self.plugin.apply_options(opts)

        self.assertEqual(args, ["album:Bends"])
        self.assertTrue(config["covers"]["pretend"].get(bool))
        self.assertEqual(config["covers"]["backup"].get(), "/backup")
        self.assertFalse(config["covers"]["embed"].get(bool))


if __name__ == "__main__":
    unittest.main()
