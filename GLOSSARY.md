# Vinyl Underground

A self-hosted music server, built as a hard fork of Navidrome. It lets a small household stream, organize, download and auto-generate listening from their own music Library, from anywhere on their private network.

## Language

### Library

**Library**:
The full collection of music on the network drive, as the app understands it.
_Avoid_: Collection, catalog

**File**:
One physical audio file on the network drive.
_Avoid_: Song file, copy

**Track**:
A playable piece of music in the Library. Each Track is backed by exactly one File, because Duplicates are removed from the drive rather than hidden in the app.
_Avoid_: Song (ambiguous between Track and File), item

**Duplicate**:
A File whose audio is identical to another File's, even if its tags or embedded art differ, usually left behind by a music app copying it (e.g. `Song 1.mp3`, `Song 2.mp3`). A Duplicate is moved to Quarantine; it is never a Track. Identical audio means identical audio data (e.g. the same MP3 frames), never just the same name, title or duration.
_Avoid_: Copy, version (a live or remastered recording is a different Track, not a Duplicate)

**Album held twice**:
An album (with its album artist) kept on the drive as two separate copies in different folders, so the app lists every Track twice. Their audio differs, so they are not Duplicates; the songs the weaker copy shares with the other are moved to Quarantine. One stray Track of an album in another folder (e.g. a mix) doesn't make it held twice, and neither does a multi-disc album with one folder per disc.
_Avoid_: Duplicate album (its Files are not Duplicates), double album (an album of two discs)

**Correction**:
An admin's fix to a wrong tag value (e.g. renaming the Artist "Greatful Dead" to "Grateful Dead"), written to the Files it fixes through the Edit journal, or to a wrong album cover (an image chosen or uploaded as the album's cover, written as the folder's `cover.jpg`, or an image marked wrong for the album, which is then skipped; only that mark is kept in the app). It never changes a Track's identity, and it never moves a File (that is Organize). Corrections kept in the app before ADR-0011, and applied whenever a File's tags were read, are migrated into the Files. Renaming an Artist to another Artist's name merges the two (an artist merge Correction). An album Correction fixes an album's title, album artist and year, and can give it a MusicBrainz release (picked with Find on MusicBrainz, whose Cover Art Archive cover then becomes the album's cover unless one is chosen), tied to the album's folder (the "Edit" button on the album page saves one). A track numbers Correction (shown as "Track order") fixes the disc and track numbers of an album's Tracks, each tied to its File, saved together from the same dialog; it is the only Correction of single Tracks.
_Avoid_: Edit, override

**Quarantine**:
The folder on the drive holding Files removed from the Library (Duplicates, and the weaker copy of each album held twice), kept until they are permanently deleted. The app moves Files there, through the Edit journal, so a move can be undone; only Empty Quarantine, after a confirmation, deletes them.
_Avoid_: Trash, recycle bin

**Organize**:
To rename and move the Library's Files into one naming scheme, `<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>` (`<D>-<NN>` on a multi-disc album), only through a plan the admin has reviewed. Folders whose names differ only in letter case are merged this way. A tag edit never moves a File by itself. The plan lists every move and the Tracks it can't place, with why; the approved plan runs as one edit job, each move journaled, the other Files of an album's folder (its `cover.jpg`) moving with it, and each Track keeping its ID.
_Avoid_: Rename, tidy, sort

**Edit journal**:
The app's permanent record of every write it makes to a File: its original tags and path, and what changed. The admin sees it, newest first, on the Edit history page. Any File, or a whole job of writes, can be undone from it, and an undo is recorded too. Each write is checked before it replaces the File (made to a temporary copy, re-read, audio data unchanged), and a File changed since the last scan is skipped. For an album cover it also keeps the image a write replaced (the folder's old `cover.jpg`, or a File's embedded picture), in the app's data folder, so an undo puts it back byte for byte.
_Avoid_: History (a Listener's listening history), log, backup (no byte copy of a File is kept)

**Embed in Files**:
The admin's explicit action that writes an album's folder cover (`cover.jpg`) into each of its Files as their first picture, as one Edit job, checked like any tag write. It is never automatic, since it rewrites every File and makes each one larger.
_Avoid_: Embed art, sync covers

**Edit job**:
A batch of writes to Files the app runs in the background, one step per File, kept so it carries on after a restart. Jobs run one at a time, so the Library is never written to in parallel. A File a job can't write (e.g. changed on disk since the last scan) is skipped, with the reason, and the rest are still written. Every write is in the Edit journal with its job, so a whole job can be undone. A single Track's edit is a job of one File, which the Edit dialog waits for.
_Avoid_: Task, batch, queue item

**Library manager**:
The app's role as the tool that edits the Library's Files (tags, covers, names, folders and Quarantine), for the admin only. It needs a writable Library: with a read-only one, editing is off and nothing falls back to an in-app overlay.
_Avoid_: Tag editor, file manager

### People

**Listener**:
A person with their own login and personal data (favorites, history, playlists) in the app.
_Avoid_: User (too generic), account, member

### Listening

**Station**:
A personal, endless, skippable stream of Tracks picked for one Listener, shaped by a DJ and an optional Theme.
_Avoid_: Radio (Navidrome's "Internet Radio" means external stream URLs), live radio, mix

**DJ**:
The persona that gives a Station its character: its taste, its tone and the on-screen text it shows between Tracks. It never speaks aloud. A DJ is either built-in or created by a Listener, and a Listener's own DJs are private to them.
_Avoid_: Host, presenter, bot

**Theme**:
An optional free-text mood or idea a Listener gives when starting a Station (e.g. "late-night drive").
_Avoid_: Prompt (reserved for Generated Playlists), genre, seed, colour theme (the app's look)

**Blurb**:
The short on-screen text a DJ shows between two Tracks on a Station.
_Avoid_: Intro, voiceover, caption

**Now Playing**:
A Listener's own view of what they are playing: full screen on a phone, or a side panel docked on the right on desktop. It holds the album art, lyrics, the queue and the player's controls.
_Avoid_: Mini player (the bar along the bottom), full-screen player, Who's Listening (what other Listeners are playing)

**Who's Listening**:
The panel showing what other Listeners are playing right now. Navidrome called it "Now Playing".
_Avoid_: Now Playing (the Listener's own view), activity

### Playlists and offline

**Playlist**:
An ordered list of Tracks owned by one Listener. It is private unless shared, and a shared Playlist is read-only to other Listeners.
_Avoid_: Queue (the current play order), list, mix

**Generated Playlist**:
A Playlist the AI created from a Listener's Prompt, and saved once the Listener accepted the preview. Apart from remembering its Prompt, it is an ordinary Playlist.
_Avoid_: AI playlist, smart playlist (a Navidrome rule-based playlist, which is a different thing)

**Prompt**:
The free-text request a Listener gives to get a Generated Playlist (e.g. "rainy Sunday 90s indie").
_Avoid_: Query, theme

**Download**:
A Playlist or album stored on a Listener's device for playback without a network connection.
_Avoid_: Offline cache, sync

### New music

**Inbox**:
The writable folder where all new music arrives (Rips, uploads and Files dropped in) before it joins the Library. The app scans it as a second library, visible only to the admin, and Tracks in it can be played straight away.
_Avoid_: Staging, downloads (a Download is offline storage on a device), import folder

**Rip**:
A File the app fetched from a web page with yt-dlp (usually YouTube), keeping the original audio stream. A Rip carries a `SOURCE` tag naming the site and a `SOURCEURL` tag holding the web page it came from. It is shown with a "Ripped" badge, because it is lower quality than most of the Library. Only a File whose `SOURCEURL` is an http(s) URL counts as a Rip: other tools may write `SOURCE` too (e.g. `SOURCE=CD` from a CD ripper), and those Files are not Rips.
_Avoid_: Download (taken: offline storage on a device), import

**Promote**:
To move reviewed Files from the Inbox into the Library's dedicated writable folder on the NAS. The Track stays the same Track, so favorites, Playlist entries and play counts carry over.
_Avoid_: Import, publish, approve
