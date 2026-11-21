# SPDX-License-Identifier: GPL-3.0-only
"""Give every album beets has retagged a cover.jpg in its folder, embedded in its Files.

`beet covers [QUERY]` goes through the albums in beets' library. For each one:

  - a cover.jpg already in its folder is kept as it is (and listed if it is small);
  - else a good cover the album already has is kept: a well-named image in its folder
    (front, folder, album...) or the art embedded in its Files;
  - else fetchart's online sources are asked, in the order of `fetchart.sources`: first the
    Cover Art Archive by the MusicBrainz IDs from the retag, then other sources;
  - else a cover the album already has that is too small (or not square) is kept;
  - else nothing is written, and the album is listed for the owner.

"Good" is fetchart's `minwidth` and `enforce_ratio`; a downloaded cover is shrunk to its
`maxwidth`. The cover is written as cover.jpg into each folder holding the album's Files (never
replacing a cover.jpg already there), then embedded, shrunk to `embed_maxwidth`, into each File that
has no embedded art. Files that already have art keep it. Only the images are written into the
Files, not beets' other tags, so hand fixes made since the retag stay. Albums in a folder beets
is told to ignore (Quarantine, the recycle bin) or under a .ndignore are never touched.

The albums left without a good cover are written to `report` as TSV, as the run goes: status
(small, none, or error for an album whose folder or Files couldn't be written), album artist,
album and folders. List this plugin before fetchart in `plugins`: it adds
fetchart's `embedded` source.

    covers:
        embed_maxwidth: 600       # pixels; 0 embeds cover.jpg as it is
        report: /state/no-cover.tsv
        embed: yes                # no: only write cover.jpg, never touch the Files (--no-embed)
        pretend: no               # yes: only say what it would do (--pretend); the report goes
                                  # beside `report`, named *-dry-run.tsv
        backup:                   # a folder outside the share (--backup DIR): see below

With a backup folder, each File is copied to `files/` in it, by its path in the share, before
the cover is embedded in it (never over an earlier copy), and `manifest.tsv` in it lists what the
run changed, as it goes: `cover<TAB>PATH<TAB>SHA256` for each cover.jpg, just before it is written,
and `file<TAB>PATH<TAB>SHA256` for each File, once the cover is in it. The checksums are of what the
run wrote, so run-covers.sh --restore can leave alone anything changed since.
"""
from __future__ import annotations

import fnmatch
import hashlib
import io
import os
import shutil
from typing import NamedTuple

import mediafile
from beets import config, plugins, ui
from beets.plugins import BeetsPlugin
from beets.util import get_temp_filename
from beetsplug import fetchart
from PIL import Image

COVER = "cover.jpg"
NDIGNORE = ".ndignore"
# The statuses of an album left without a good cover, which go in the report.
REPORTED = ("small", "none", "error")


class Result(NamedTuple):
    status: str  # kept, found, small, none, error, or left out
    source: str = ""  # where the cover came from, or what went wrong


class EmbeddedArt(fetchart.LocalArtSource):
    """fetchart source for the art already embedded in an album's Files (the first File with any)."""
    NAME = "Embedded art"
    ID = "embedded"

    def get(self, album, plugin, paths):
        if paths is None:  # fetchart -f: online sources only
            return
        for item in album.items():
            try:
                art = mediafile.MediaFile(os.fsdecode(item.path)).art
            except (OSError, mediafile.UnreadableFileError) as exc:
                self._log.debug("could not read {}: {}", item.filepath, exc)
                continue
            if art:
                ext = mediafile.image_extension(art) or "jpg"
                temp = get_temp_filename(__name__, suffix=f".{ext}")
                with open(temp, "wb") as fh:
                    fh.write(art)
                yield self._candidate(path=os.fsencode(temp), match=fetchart.MetadataMatch.EXACT)
                return

    def cleanup(self, candidate) -> None:
        if candidate.path:
            os.remove(candidate.path)


fetchart.ART_SOURCES.add(EmbeddedArt)


def _jpeg(path: str | bytes, quality: int, maxwidth: int = 0) -> bytes:
    """The image at `path` as JPEG, no wider or taller than `maxwidth`; as it is if it already fits."""
    with open(path, "rb") as fh:
        data = fh.read()
    with Image.open(io.BytesIO(data)) as im:
        if im.format == "JPEG" and (not maxwidth or max(im.size) <= maxwidth):
            return data
        im = im.convert("RGB")
        if maxwidth:
            im.thumbnail((maxwidth, maxwidth), Image.Resampling.LANCZOS)
        out = io.BytesIO()
        im.save(out, "JPEG", quality=quality or 75)
        return out.getvalue()


def _cover_in(folder: str) -> str | None:
    """The folder's cover.jpg, whatever its case (the NAS matches names without case)."""
    return next((os.path.join(folder, n) for n in sorted(os.listdir(folder)) if n.casefold() == COVER), None)


class CoversPlugin(BeetsPlugin):
    def __init__(self) -> None:
        super().__init__()
        self.config.add({"embed_maxwidth": 600, "report": None, "embed": True, "pretend": False, "backup": None})
        self._fetchart: fetchart.FetchArtPlugin | None = None

    @property
    def fetchart(self) -> fetchart.FetchArtPlugin:
        if self._fetchart is None:
            found = [p for p in plugins.find_plugins() if isinstance(p, fetchart.FetchArtPlugin)]
            if not found:
                raise ui.UserError("covers needs the fetchart plugin")
            self._fetchart = found[0]
        return self._fetchart

    @fetchart.setter
    def fetchart(self, plugin: fetchart.FetchArtPlugin) -> None:
        self._fetchart = plugin

    def commands(self) -> list[ui.Subcommand]:
        cmd = ui.Subcommand("covers", help="give each album a cover.jpg, embedded in its Files")
        cmd.parser.add_option("-n", "--pretend", action="store_true", help="only say what it would do")
        cmd.parser.add_option("-b", "--backup", metavar="DIR",
                              help="back up each File before embedding, and list what changed, in DIR")
        cmd.parser.add_option("--no-embed", dest="embed", action="store_false",
                              help="only write cover.jpg; never touch the Files")

        def func(lib, opts, args) -> None:
            self.apply_options(opts)
            self.cover_all(lib.albums(args))

        cmd.func = func
        return [cmd]

    def apply_options(self, opts) -> None:
        """Let the command's options override the config."""
        if opts.pretend:
            self.config["pretend"] = True
        if opts.backup:
            self.config["backup"] = opts.backup
        if opts.embed is False:
            self.config["embed"] = False

    @property
    def pretend(self) -> bool:
        return self.config["pretend"].get(bool)

    @property
    def embed(self) -> bool:
        return self.config["embed"].get(bool)

    @property
    def backup_dir(self) -> str | None:
        """The backup folder, or None for no backup."""
        return self.config["backup"].as_filename() if self.config["backup"].get() else None

    def cover_all(self, albums) -> None:
        counts: dict[str, int] = {}
        report = self.config["report"].get()
        if report and self.pretend:
            report = os.path.splitext(report)[0] + "-dry-run.tsv"
        root = self._root()
        with open(report or os.devnull, "w", encoding="utf-8") as fh:
            fh.write("status\talbum artist\talbum\tfolders\n")
            for album in albums:
                result = self.cover(album)
                counts[result.status] = counts.get(result.status, 0) + 1
                ui.print_(f"{album}: {result.status}" + (f" ({result.source})" if result.source else ""))
                if result.status in REPORTED:
                    folders = " | ".join(os.path.relpath(f, root) for f in self._folders(album))
                    fh.write(f"{result.status}\t{album.albumartist}\t{album.album}\t{folders}\n")
                    fh.flush()
        ui.print_(", ".join(f"{n} {status}" for status, n in sorted(counts.items())) or "no albums")
        if self.pretend:
            ui.print_("dry run: nothing was written")
        if report:
            listed = sum(counts.get(s, 0) for s in REPORTED)
            ui.print_(f"{listed} albums without a good cover listed in {report}")

    def cover(self, album) -> Result:
        """Give `album` a cover.jpg in each of its folders and embed it in its Files (see above)."""
        folders = self._folders(album)
        if not folders or any(self._left_out(f) for f in folders):
            return Result("left out")
        temps: list[bytes] = []
        try:
            existing = next((c for f in folders if (c := _cover_in(f))), None)
            if existing:
                with Image.open(existing) as im:
                    good = im.size[0] >= self.fetchart.minwidth
                result, source = Result("kept" if good else "small", COVER), existing
            else:
                result, candidate = self._find(album, folders)
                if candidate is None:
                    return result
                source = candidate.path
                if os.path.dirname(source) not in [os.fsencode(f) for f in folders]:
                    temps.append(source)
            if self.pretend:
                return result
            covers = [self._write_cover(f, source) for f in folders]
            album.artpath = os.fsencode(covers[0])
            album.store()
            if not self.embed:
                return result
            if failed := self._embed(album, covers[0]):
                return Result("error", "could not embed the cover in " + ", ".join(failed))
            return result
        except OSError as exc:
            self._log.warning("{}: {}", album, exc)
            return Result("error", str(exc))
        finally:
            for t in temps:
                if os.path.exists(t):
                    os.remove(t)

    def _find(self, album, folders: list[str]):
        """A good cover for the album, else a small one it already has, with what was found."""
        art = self.fetchart
        paths = [os.fsencode(f) for f in folders]
        candidate = art.art_for_album(album, paths)
        if candidate:
            local = candidate.source_name in ("filesystem", EmbeddedArt.ID)
            return Result("kept" if local else "found", candidate.source_name), candidate
        minwidth, enforce_ratio = art.minwidth, art.enforce_ratio
        art.minwidth, art.enforce_ratio = 0, False
        try:
            candidate = art.art_for_album(album, paths, local_only=True)
        finally:
            art.minwidth, art.enforce_ratio = minwidth, enforce_ratio
        if candidate:
            return Result("small", candidate.source_name), candidate
        return Result("none"), None

    def _write_cover(self, folder: str, source: str | bytes) -> str:
        """The folder's cover.jpg, written from `source` unless it has one."""
        if existing := _cover_in(folder):
            return existing
        path = os.path.join(folder, COVER)
        data = _jpeg(source, self.fetchart.quality)
        # Listed first, so a run stopped mid-write still lists it (a restore skips one not there).
        self._record("cover", path, hashlib.sha256(data).hexdigest())
        temp = path + ".part"
        with open(temp, "wb") as fh:
            fh.write(data)
        shutil.move(temp, path)
        return path

    def _backup(self, path: str) -> None:
        """Copy the File at `path` into the backup folder, if there is one, before it changes."""
        if not self.backup_dir:
            return
        copy = os.path.join(self.backup_dir, "files", self._path_in_share(path))
        if not os.path.exists(copy):
            os.makedirs(os.path.dirname(copy), exist_ok=True)
            shutil.copy2(path, copy + ".part")
            os.replace(copy + ".part", copy)

    def _record(self, kind: str, path: str, digest: str) -> None:
        """Add a line for `path`, as the run wrote it, to the backup folder's manifest, if there is one."""
        if not self.backup_dir:
            return
        os.makedirs(self.backup_dir, exist_ok=True)
        with open(os.path.join(self.backup_dir, "manifest.tsv"), "a", encoding="utf-8") as fh:
            fh.write(f"{kind}\t{self._path_in_share(path)}\t{digest}\n")

    def _path_in_share(self, path: str) -> str:
        return os.path.relpath(os.path.abspath(path), self._root())

    def _embed(self, album, cover: str) -> list[str]:
        """Embed `cover` in each of the album's Files with no art; the names of those it couldn't."""
        data, failed = None, []
        for item in album.items():
            path = os.fsdecode(item.path)
            try:
                mf = mediafile.MediaFile(path, id3v23=config["id3v23"].get(bool))
                if mf.images:
                    continue
                data = data or _jpeg(cover, self.fetchart.quality, self.config["embed_maxwidth"].get(int))
                mf.images = [mediafile.Image(data, type=mediafile.ImageType.front)]
                self._backup(path)
                mf.save()
                if self.backup_dir:
                    with open(path, "rb") as fh:
                        self._record("file", path, hashlib.sha256(fh.read()).hexdigest())
            except (OSError, mediafile.UnreadableFileError) as exc:
                self._log.warning("could not embed the cover in {}: {}", path, exc)
                failed.append(os.path.basename(path))
        return failed

    @staticmethod
    def _folders(album) -> list[str]:
        return sorted({os.path.dirname(os.fsdecode(i.path)) for i in album.items()})

    @staticmethod
    def _root() -> str:
        return os.path.abspath(config["directory"].as_filename())

    def _left_out(self, folder: str) -> bool:
        """Whether `folder` is outside the Library: outside the share, in a folder beets ignores
        (Quarantine, the recycle bin), or under a .ndignore."""
        root = self._root()
        rel = os.path.relpath(os.path.abspath(folder), root)
        if rel == ".." or rel.startswith("../"):
            return True
        parts = [] if rel == "." else rel.split(os.sep)
        ignore = config["ignore"].as_str_seq()
        if any(fnmatch.fnmatch(p, pat) for p in parts for pat in ignore):
            return True
        return any(os.path.exists(os.path.join(root, *parts[:i], NDIGNORE)) for i in range(len(parts) + 1))
