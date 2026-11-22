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
An admin's fix to a wrong tag value (e.g. renaming the Artist "Greatful Dead" to "Grateful Dead"), kept in the app and applied whenever a File's tags are read. It is never written to the File, and it never changes a Track's identity. Renaming an Artist to another Artist's name merges the two (an artist merge Correction).
_Avoid_: Edit, override, retag (that writes the Files)

**Quarantine**:
The folder on the drive holding Files removed from the Library (Duplicates, and the weaker copy of each album held twice), kept until they are permanently deleted.
_Avoid_: Trash, recycle bin

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
