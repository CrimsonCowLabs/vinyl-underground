# Library cleanup

Tools for cleaning up a music share before the app scans it. The app itself has no dedupe code ([ADR-0002](../../docs/adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)); these run by hand, through a temporary writable mount, and the app keeps its read-only mount throughout.

| Tool | What it does |
|---|---|
| `scan.py` | Lists every folder and File on the share, serially, as TSV |
| `casemerge.py` | Plans merges of sibling names that differ only by letter case (pure, unit-tested) |
| `dupes.py` | Plans moving Duplicates into Quarantine from a `jdupes` report (pure, unit-tested) |
| `audiodupes.py` | Plans moving Duplicates whose tags differ (`Song 1.mp3` beside `Song.mp3`) into Quarantine, by comparing their audio (pure planner, unit-tested) |
| `heldtwice.py` | Plans moving the weaker copy of each album held twice (two copies in different folders) into Quarantine, from the app's database and a scan (pure planner, unit-tested) |
| `audio.py` | Finds the audio in an MP3, M4A, AIFF or WAV File, leaving out its tags and embedded art (unit-tested) |
| `apply.py` | Carries out a plan, checking each step by re-listing both folders |
| `health.py` | Reports how healthy the Library is, from the app's database, read-only (pure, unit-tested) |
| `retag.py` | Lists the folders beets should retag, leaving out Quarantine, the recycle bin and `.ndignore` folders (pure, unit-tested) |
| `beets/` | beets in Docker, set up to retag Files in place from MusicBrainz (`retag.sh`, config, two small plugins) |

Python 3.10+ only, no dependencies. Run the tests with `python3 -m unittest` in this folder. beets runs in Docker, and the tests of its plugins (in `beets/`) run in its image, with `beets/retag.sh test`.

## Why names that differ only by case need merging

Some NAS devices (old Buffalo LinkStations over SMB1, for example) look names up case-insensitively and cache the lookups. For two siblings such as `ATB` and `Atb`, both paths can lead into the *same* real folder, so a listing of either side shows the same Files and scans silently miss the other folder's contents. Mounting with SMB1 Unix extensions would make names case-sensitive, but these devices don't support them.

You can't merge such a pair by moving Files from one side to the other, because you can't tell which folder a path really reaches. So the merge has two phases:

- **Phase A: rename.** All but one member of each clash gets a unique temporary name (`Atb [case-merge]`). A rename can't lose data, whichever real folder the server applies it to. Clashes nested inside clashing folders only become visible once their parents have unique names, so Phase A repeats until a scan finds none.
- **Phase B: merge.** With every name unique, listings are trustworthy. Each `[case-merge]` sibling is merged into its partner. The side with more Files wins, so fewer Files move. A same-name File of the same size is planned for Quarantine, and `apply.py` moves it only after confirming it is byte-identical to the File it duplicates. Anything else is left for the owner to decide.

## Recipe: merge names that differ only by case

`$PLANS` is any folder outside the share for scans and plans. Never edit a plan after `apply.py` has started on it, because its progress log records step numbers.

**1. Mount the share writable alongside the read-only mount.** Use the same credentials file and SMB version as the normal mount (`NAS` and the credentials path are placeholders):

```bash
sudo mkdir -p /mnt/music-rw
sudo mount -t cifs //NAS/music /mnt/music-rw \
  -o rw,vers=1.0,credentials=/path/to/smb-credentials,uid=$(id -u),gid=$(id -g),file_mode=0644,dir_mode=0755,iocharset=utf8,noserverino
```

**2. Phase A, repeated until it plans nothing:**

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 casemerge.py rename $PLANS/scan.tsv > $PLANS/rename-1.tsv   # review it
python3 apply.py /mnt/music-rw $PLANS/rename-1.tsv --dry-run
python3 apply.py /mnt/music-rw $PLANS/rename-1.tsv
```

Then rescan and plan again (`rename-2.tsv`, …) until `casemerge.py` reports `0 clashing groups`.

**3. Phase B:**

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 casemerge.py merge $PLANS/scan.tsv > $PLANS/merge.tsv
```

`casemerge.py merge` refuses to plan while any names still clash. Review `merge.tsv` before applying it, and decide each `decide` line by hand. To keep one copy and drop the other, change the line to `set-aside<TAB>SRC<TAB>_Quarantine/KEEP<TAB>KEEP`: `apply.py` then moves SRC into Quarantine without the byte check, provided KEEP exists. Use the same change when `apply.py` stops because a planned quarantine isn't byte-identical (e.g. `.DS_Store` files, or copies whose tags differ). Changing a line that hasn't run yet keeps the step numbers, so the run can resume. Then run `apply.py` on it as in step 2. If `apply.py` stops on a step that won't verify, or a pair can't be addressed reliably over SMB at all, merge that pair on the NAS directly instead (e.g. through its web file manager), rescan, and continue.

**4. Check.** Two consecutive scans must finish without errors and return identical lists, with no clashes and no `[case-merge]` names left:

```bash
python3 scan.py /mnt/music-rw > $PLANS/check-1.tsv && \
python3 scan.py /mnt/music-rw > $PLANS/check-2.tsv && \
cmp $PLANS/check-1.tsv $PLANS/check-2.tsv && echo "scans identical"
python3 casemerge.py rename $PLANS/check-1.tsv > /dev/null   # must report "0 clashing groups"
grep -c 'case-merge' $PLANS/check-1.tsv                       # must print 0
```

Some NAS devices keep a recycle bin on the share (`trashbox/` on Buffalo LinkStations), and removing a folder over SMB moves it there instead of deleting it. The emptied `[case-merge]` folders end up there, so ignore that folder in step 4 and empty it from the NAS's own admin page.

**5. Quarantine the Duplicates** (next recipe) while the writable mount is still there, or unmount it now: `sudo umount /mnt/music-rw`. The app's read-only mount was never changed.

## Recipe: quarantine Duplicates

Run this after the case merge, once two consecutive scans are identical: `jdupes` can't be trusted on names that still clash. It needs the writable mount from step 1 above, and `jdupes` on the host. Re-run it whenever new Duplicates may have arrived.

**1. List the Duplicate sets.** `jdupes` without an action flag changes nothing, so run it on the read-only mount. Run it from the share root so its paths are relative, and leave out `_Quarantine/` and the NAS's recycle bin (`trashbox/` here):

```bash
(cd /mnt/music && find . -mindepth 1 -maxdepth 1 -type d ! -name _Quarantine ! -name trashbox -printf '%P\0' \
  | xargs -0 jdupes -r -S) > $PLANS/jdupes.txt
python3 scan.py /mnt/music > $PLANS/scan.tsv
```

**2. Plan, then review.**

```bash
python3 dupes.py $PLANS/jdupes.txt $PLANS/scan.tsv --root /mnt/music > $PLANS/dupes.tsv
```

Each line is `quarantine<TAB>SRC<TAB>_Quarantine/SRC<TAB>KEEP`: SRC moves to the same relative path under `_Quarantine/`, and KEEP is the byte-identical File that stays. Sets with no audio Files (a DVD's matching `.IFO` and `.BUP`, cover art) are left alone. So are Files under `_Quarantine/` or any folder with a `.ndignore`, such as the recycle bin once you give it one: the app's scanner skips those folders, so they are not part of the Library. Give the recycle bin a `.ndignore` (`touch /mnt/music-rw/trashbox/.ndignore`) before the first scan, or it becomes Tracks. Which File is kept, in order:

1. Not a copy's name: `Song 1`, `Song (1)` or `Copy of Song`, when `Song` itself is in the set.
2. The deepest folder, so `Artist/Album/Song` beats a loose `Song`. The bytes, and so the tags the app shows, are identical; this only decides where the File lives.
3. The shortest file name, then the first in alphabetical order.

The summary on stderr shows how many audio Files move out of each top-level folder, and marks the folders the plan empties. To keep a different File in a set, swap its SRC and KEEP and change the destination to `_Quarantine/` + the new SRC. Delete a line to keep both Files.

**3. Apply.**

```bash
python3 apply.py /mnt/music-rw $PLANS/dupes.tsv --dry-run
python3 apply.py /mnt/music-rw $PLANS/dupes.tsv
```

Before each move, `apply.py` checks again that SRC is byte-identical to KEEP and stops if not, so a report that has gone stale can't move a File that isn't a Duplicate. The move itself is a rename on the NAS, but the check reads both Files over the network, so it reads about twice the Duplicates' total size: hours for tens of GB over SMB1. It is resumable: re-run the same command after a stop. `_Quarantine/` gets a `.ndignore`, so the app's scanner skips it.

**4. Check.** Repeat step 1 and plan again. `dupes.py` must report `0 Duplicates to quarantine`.

**5. Unmount the writable mount**, unless the next recipe follows: `sudo umount /mnt/music-rw`. Then rescan in the app; a Full Scan marks the moved Files as missing. Files only leave Quarantine when you delete them by hand.

## Recipe: quarantine Duplicates whose tags differ

`jdupes` only finds Files with identical bytes. A music app that copies a File next to itself (`09 Car Alarm 1.mp3` beside `09 Car Alarm.mp3`) often rewrites the new File's tags or art, so it survives the recipe above and the app shows the same Track twice. `audiodupes.py` finds those by comparing the audio alone. Run it after the recipe above (it leaves Files in different folders to `jdupes`), with the same writable mount, and re-run it whenever new Duplicates may have arrived.

What counts as the audio, per format (`audio.py`):

- **MP3:** everything between the leading ID3v2 tags and the trailing ID3v1, APEv2, Lyrics3 and appended ID3v2 tags, so the MP3 frames, including any Xing/LAME header frame.
- **M4A/MP4:** the media data (`mdat`), without the `moov` metadata, which holds the tags and art.
- **AIFF/WAV:** the format and sample chunks (`COMM`/`SSND`, `fmt `/`data`), without tag chunks such as `ID3 `, `NAME` or `LIST`.
- **Anything else** (FLAC, Ogg, WMA, …) is listed as `not compared` and never planned.

**1. Scan and plan, then review.** Planning only reads, so it runs on the read-only mount:

```bash
python3 scan.py /mnt/music > $PLANS/scan.tsv
python3 audiodupes.py $PLANS/scan.tsv --root /mnt/music > $PLANS/audiodupes.tsv
```

Only candidates are compared: each audio File with the copy-named Files beside it (same folder and extension, e.g. `Song 1.mp3`, `Song (1).mp3`, `Copy of Song.mp3` beside `Song.mp3`). Their tag headers are read first, and only Files whose audio is the same size are read in full, so planning reads about the candidates' total size (roughly 30 GB and half an hour over SMB1 for 2,000 pairs). A copy-named File whose audio differs (a different recording or encoding that got the same name) is counted on stderr and left alone. Each line is `quarantine-audio<TAB>SRC<TAB>_Quarantine/SRC<TAB>KEEP`, and the File kept is chosen by the same rules as above, so the copy's name goes. Edit the plan as above: swap SRC and KEEP to keep the other File, or delete a line to keep both.

**2. Apply.**

```bash
python3 apply.py /mnt/music-rw $PLANS/audiodupes.tsv --dry-run
python3 apply.py /mnt/music-rw $PLANS/audiodupes.tsv
```

Before each move, `apply.py` checks again that SRC's audio is identical to KEEP's, and stops if not (or if either can't be read or compared). Like the byte check, this reads both Files in full. It is resumable: re-run the same command after a stop.

**3. Check.** Scan and plan again: `audiodupes.py` must report `0 Duplicates to quarantine`. Then unmount the writable mount (`sudo umount /mnt/music-rw`), unless the next recipe follows, and run a Full Scan in the app.

## Recipe: quarantine the weaker copy of albums held twice

Some albums are on the share twice, as two separate copies in different folders (The Bends in both `Radiohead/Albums/` and `iTunes_Library/Music/Radiohead/`), and the app lists every Track twice. Their audio differs, so they aren't Duplicates. `heldtwice.py` lists each such album once, suggests the copy to keep, and `apply.py` moves the other copy's songs into Quarantine. Run it after the audio-identical Duplicate pass above, and after a Full Scan, because it reads the app's database: a leftover `Song 1.mp3` would otherwise be left out of the plan and hold the album in two folders still.

**1. Scan and plan, then review.** Planning reads the database (read-only) and a scan, never the Files, so it is quick:

```bash
python3 scan.py /mnt/music > $PLANS/scan.tsv
python3 heldtwice.py /path/to/navidrome.db $PLANS/scan.tsv > $PLANS/heldtwice.tsv
```

What counts as an album held twice, and what is left to you:

- The same album and album artist with Tracks in more than one folder, as `health.py`'s `albums-twice` counts it (an album whose folders each hold a different disc doesn't count).
- A folder holds a **copy** only if it holds two or more of the album's Tracks and the album is most of the folder. A lone Track, or a few in a mix or a folder of loose Tracks, is a stray: stderr lists it as `stray, left to you`, and nothing in that folder is planned. Copies that share most Tracks form a group, so two copies of a two-disc album held as `CD1/` and `CD2/` folders are planned disc by disc.
- Tracks match by artist and title, ignoring case, spaces and punctuation, never by File name; failing that, by title without a bracketed subtitle (`2 + 2 = 5 (The Lukewarm.)` and `2+2=5`), if both are the same length to within 3 seconds, so a remix never matches the original. Two copies sharing no more than half the smaller copy's Tracks (iTunes splitting one copy by Track artist, say) are listed as `not planned` instead.
- Only Files the scan lists at the size the app recorded count, so run a Full Scan first; Files changed since are listed and left out. Copy-named Files are left to the Duplicate passes.

The copy kept is judged on the Tracks both copies hold: more lossless Files (FLAC, WAV, AIFF, ALAC), then the higher average bitrate, then the more complete tags (artist, album artist, year, track number, genre, MusicBrainz ID), then a cover (embedded, or an image in the folder); then the copy with more Tracks, then the first folder. Each line says why, and the summary on stderr shows, per album, the folder kept, the folder quarantined, how many Files move and how many stay. A line looks like:

```
held-twice  keep  FOLDER  quarantine  FOLDER  ALBUM  WHY  TRACK  TRACK ...
```

Each TRACK is `NAME/SIZE/NAME/SIZE`, the Track's File in the first folder and in the second. To keep the other copy, swap the words `keep` and `quarantine`. Delete a line to keep both copies. A disc held three times gets two lines keeping the same copy; to keep another, change both lines, or apply one and plan again. Only the TRACK Files of the folder marked `quarantine` move, so a Track only that copy has stays in the Library, as do its non-audio Files (cover art, playlists).

**2. Apply.**

```bash
python3 apply.py /mnt/music-rw $PLANS/heldtwice.tsv --dry-run
python3 apply.py /mnt/music-rw $PLANS/heldtwice.tsv
```

Each File moves to the same path under `_Quarantine/`. Before moving any File of an album, `apply.py` checks that every File it will move, and the File kept in its place, is still listed at the size it had when planned, and moves nothing if not, so a stale plan can't move a File that was retagged or replaced, or one whose Track the copy kept no longer holds. The audio can't be compared, because it differs by definition, and the size check reads no audio, so the run is quick. Each move is checked by re-listing both folders. It is resumable: re-run the same command after a stop, and Files already in Quarantine at their planned size count as moved.

**3. Check.** Unmount the writable mount (`sudo umount /mnt/music-rw`), run a Full Scan in the app, then scan and plan again: `heldtwice.py` must report `0 copies of albums held twice to quarantine`. `health.py`'s `albums-twice` still counts the strays and the albums listed as `not planned`; move or retag those by hand.

## Recipe: retag the Library from MusicBrainz

Most Files have never been matched against a music database, so many lack a year, a track number or MusicBrainz IDs, and some carry wrong disc numbers or albums. [beets](https://beets.io/) matches each album against MusicBrainz, using audio fingerprints (AcoustID) where the tags are too poor to search on, shows you each match to approve, and writes the tags into the Files in place. Run it after the Quarantine recipes above, and after a Full Scan, so no Duplicates (`Song 1.mp3`) or albums held twice are left: beets would take them for extra Tracks, and `retag.py` warns if it finds copy-named Files. Favorites, play counts and Playlist entries may reset for retagged Tracks.

What it does and doesn't do:

- **Runs in Docker only** (`beets/`): beets 2.14.1 with every Python dependency pinned, plus Chromaprint and ffmpeg for the fingerprints. Nothing is installed on the host. The container runs as you, so it leaves no root-owned files.
- **Tags Files in place.** It writes through the temporary writable mount, and never copies, moves, renames or deletes a File (`copy`, `move`, `link` and `delete` are all off, and beets' own folder layout is never used). Tags it doesn't set, and embedded art, stay. Tags are written as ID3v2.4.
- **Sets** artist, album artist, album, title, year, track and disc numbers (restarting on each disc), genre, the MusicBrainz IDs (recording, release, release group, artists) and the AcoustID. The year is that of the release matched (The Bends' 1995 CD); the first release's date goes in the original date tags, because MusicBrainz dates some first releases by a promo (The Bends: 1994).
- **Genre** comes from the `lastgenre` plugin: the album's most popular Last.fm tag that is a known genre, made canonical (e.g. `Alternative Rock`), else the artist's. It replaces the genre the File had (mostly iTunes' broad `Rock` or `Other`); for an album Last.fm knows nothing about, the genre is cleared, so fix those by hand. MusicBrainz genres were the alternative, but many releases have none and beets would write all of a release's genres as a list.
- **Asks about every album** (`timid`), showing the release matched and each Track's changes. Albums with no match at least 75% similar are skipped without asking, by the `lowconfidence` plugin, and listed in `$RETAG_STATE/import.log` for a later review, as are the albums you skip.
- **Fingerprints every File** (the `chroma` plugin), reading up to two minutes of each File's audio over the network. beets 2.14.1 ignores fingerprints when pairing Files with Tracks, so for Files without titles it pairs them by length alone and swaps Tracks of similar length; the `fingerprints` plugin fixes that.
- **Leaves out** Quarantine, the NAS's recycle bin (`trashbox/`) and every folder with a `.ndignore`: `retag.py` never gives beets a folder that holds one, and beets' `ignore` setting skips the first two by name as well. Audio Files loose beside such a folder (or at the share root) can't be given as an album, so `retag.py` lists them on stderr to retag by hand.
- **Can be stopped and resumed.** beets keeps its own database, resume state and log in `$RETAG_STATE`, a folder outside the share: keep it for re-runs. An album you have decided on (applied or skipped) is not asked about again.
- **Keeps to the services' rate limits**: beets makes at most one MusicBrainz request a second. Its requests identify beets, the program making them (`beets/2.14.1 https://beets.io/`), never you. MusicBrainz, AcoustID and Last.fm see the names and fingerprints looked up.

**1. Set up** (once), from this folder:

```bash
beets/retag.sh build                 # build the image
beets/retag.sh test                  # run its plugins' tests in the image
export RETAG_STATE=~/retag-state     # any folder outside the share; keep it
python3 health.py /path/to/navidrome.db > $PLANS/health-before.txt
```

Then mount the share writable at `/mnt/music-rw`, as in step 1 of the case merge (set `MUSIC_RW` if you mount it elsewhere).

**2. Trial on about 10 albums.** Pick albums with problems, e.g. from `health.py --list suspect-discs` and `--list no-year`, and give their folders relative to the share root:

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 retag.py $PLANS/scan.tsv "iTunes_Library/Music/Radiohead/The Bends" "ANOTHER/ALBUM" ... > $PLANS/trial.txt
beets/retag.sh import $PLANS/trial.txt
```

For each album, beets shows the release it matched and the changes it would make, then asks:

| Key | Does |
|---|---|
| `A` (or Enter) | Apply the match: write the tags |
| `M` | Show more candidates, to pick another release (another country or edition) by number |
| `S` | Skip the album for now; it is logged for the review |
| `E` / `I` | Search MusicBrainz for another artist and album / give a release ID or URL from musicbrainz.org |
| `B` | Stop. Re-run the same command later to resume |

Then unmount the writable mount (`sudo umount /mnt/music-rw`), run a Full Scan in the app, and check the trial albums there: names, years, Track order, one disc each (unless they really have more), genres. If something is wrong, change [`beets/config.yaml`](beets/config.yaml) before going on.

**3. Full run.** Mount the writable mount again, then:

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 retag.py $PLANS/scan.tsv > $PLANS/retag.txt
beets/retag.sh import $PLANS/retag.txt
```

The trial albums aren't asked about again. Expect it to take many hours: each album waits for MusicBrainz, and each File is fingerprinted over SMB1. Stop with `B` (or Ctrl-C) whenever you like, and re-run the same command to carry on; an album stopped halfway is asked about again.

**4. Review the skipped albums.** `grep '^skip ' $RETAG_STATE/import.log` lists every album skipped in steps 2 and 3 (including those you have since applied in a review). Go through them with:

```bash
beets/retag.sh review
```

It asks about every skipped album, even those with no good match, so you can pick a candidate by number, search with `E`, give a release with `I`, or skip it again (it then comes up in the next review). An album applied here isn't asked about again. Fix albums that aren't on MusicBrainz by hand.

**5. Check.** Unmount the writable mount (`sudo umount /mnt/music-rw`), run a Full Scan in the app, and compare the health report with step 1's: `no-year`, `no-track-number`, `no-album`, `no-artist` and `no-musicbrainz-id` should be far lower, and `suspect-discs` near 0.

```bash
python3 health.py /path/to/navidrome.db > $PLANS/health-after.txt
diff $PLANS/health-before.txt $PLANS/health-after.txt
```

**Re-running for new music.** Repeat steps 3 and 4 with the same `RETAG_STATE`: only folders beets hasn't seen are asked about. beets remembers folders, not Files, so a File added to a folder already done isn't retagged; fix that one by hand. Deleting `$RETAG_STATE` starts over and asks about every album again.

**Testing on a scratch copy.** `RETAG_QUIET=1 beets/retag.sh import LIST` applies strong matches (at least 96% similar) and skips the rest without asking. Use it only on a copy of a few albums, with `MUSIC_RW` set to the copy and `RETAG_STATE` to a throwaway folder, never on the Library: nothing is approved. `retag.sh` refuses it when `MUSIC_RW` is a mount.

## Library health report

`health.py` prints how healthy the Library is, from the app's database, so each cleanup can show its numbers before and after. It opens the database read-only, so it is safe to run on the live database while the app runs, or on a copy; it never touches the share.

```bash
python3 health.py /path/to/navidrome.db                        # every number, and the key that lists it
python3 health.py /path/to/navidrome.db --list suspect-discs   # the offenders behind one number, as TSV
```

| Key | What it counts |
|---|---|
| `tracks`, `albums` | Tracks and albums in total (totals only, nothing to list) |
| `duplicates` | Likely Duplicates: the extra Files sharing artist, title and album (ignoring case), and how many groups they form |
| `copy-named` | Of those, the copy-named Files: `Song 1`, `Song (1)` or `Copy of Song` beside `Song` |
| `albums-twice` | The same album and album artist in more than one folder, unless each folder holds a different disc |
| `suspect-discs` | Albums with a disc of one or two Tracks while the rest have no disc number |
| `no-cover` | Albums with no embedded art and no image in any of their folders |
| `no-year`, `no-album`, `no-artist`, `no-track-number`, `no-musicbrainz-id` | Tracks missing that tag; a blank, `Unknown` or `[Unknown …]` album or artist counts as missing |

Only Tracks and albums the app still has count: Files gone from the share since the last scan are left out. Likely Duplicates are a hint, not proof: only Files whose audio is identical are Duplicates, and those go through the Quarantine recipes above. They need the same album too, so a live or compilation version of a song isn't counted; grouping by artist and title alone gives a larger number. An album also counts as held twice when one stray Track of it sits in another folder, such as a mix; `--list albums-twice` shows the folders.
