import { useCallback, useEffect, useState } from 'react'
import { useDataProvider } from 'react-admin'

// The pairing's own failures are MusicBrainz's codes, or the album's.
const knownMatchErrors = ['busy', 'unavailable', 'disabled', 'notFound']

// The options a save with a picked release starts with: the release's IDs, its cover and the
// album's MusicBrainz genres, keeping what the Files hold otherwise (#170).
const noReleaseOptions = {
  canonical: {
    album: false,
    albumArtist: false,
    date: false,
    titles: false,
    artists: false,
  },
  replaceTrackIds: false,
  cover: true,
  genres: true,
}

// Why a picked release's cover, when it was asked for, wasn't written: the Cover Art Archive had
// none or couldn't be asked, or the album has no folder of its own to write it in.
const pickCoverNotes = {
  none: 'resources.album.musicbrainz.noCover',
  failed: 'resources.album.musicbrainz.coverFailed',
  disabled: 'resources.album.musicbrainz.coverDisabled',
  noFolder: 'resources.album.musicbrainz.coverNoFolder',
}

// What came of an edit job on the album, from the server's answer: whether it is done (or
// finishes in the background, see the Activity panel), the album's ID after it (which changes
// with the album's values), and the note worth a word over the caller's usual one (null if
// none), with the options to notify it with. Files the job couldn't write (the Activity panel
// lists them, and why), or a picked release whose cover couldn't be had, are worth one. A
// release that couldn't reach every File was taken out of those it reached, so the album isn't
// split: nothing changed.
export const applyOutcome = (data) => {
  const skipped = data.job?.skipped || 0
  const note = data.rolledBack
    ? 'resources.album.musicbrainz.rolledBack'
    : skipped
      ? 'resources.album.editor.skipped'
      : pickCoverNotes[data.pickCover] || null
  const type = note ? 'warning' : 'info'
  return {
    done: !!data.done,
    albumId: data.albumId,
    note,
    notifyOptions: skipped
      ? { type, messageArgs: { smart_count: skipped } }
      : { type },
  }
}

// picked is the flow's state with the release r picked (null for none) for the album albumId: not
// paired yet, and the options a picked release starts with.
const picked = (r, albumId) => ({
  albumId,
  release: r,
  match: null,
  matchError: null,
  options: noReleaseOptions,
})

// The release flow for an album (its id, trackCount, artist, title and compilation, as they
// stand where it is offered: the album Edit dialog's fields, say): the release picked with Find
// on MusicBrainz, how the album's Tracks pair with it (null while it loads), why that couldn't
// be had (an error code), and what the admin ticks. Shown with <ReleaseLookup lookup={...} />.
//
// The release, its pairing, its error and its options are one state, so picking another release
// (or another album being looked up) drops the last one's all at once: no render, and so no
// apply, ever has a release with another's pairing or options.
export const useReleaseLookup = (album) => {
  const dataProvider = useDataProvider()
  const [state, setState] = useState(() => picked(null, album.id))
  // An album looked up afresh (its ID changes with its tags) starts over.
  const current = state.albumId === album.id ? state : picked(null, album.id)
  const { release, match, matchError, options } = current
  const pick = useCallback((r) => setState(picked(r, album.id)), [album.id])
  const setOptions = useCallback(
    (o) => setState((s) => ({ ...s, options: o })),
    [],
  )

  useEffect(() => {
    if (!release) return
    let active = true
    // Taken only while the same release is still picked for the same album.
    const settle = (fields) =>
      active &&
      setState((s) =>
        s.release === release && s.albumId === album.id
          ? { ...s, ...fields }
          : s,
      )
    dataProvider
      .matchMusicBrainzRelease(release.id, album.id)
      .then(({ data }) => settle({ match: data }))
      .catch((err) => {
        const code = err?.body?.error
        settle({
          matchError: knownMatchErrors.includes(code) ? code : 'unknown',
        })
      })
    return () => {
      active = false
    }
  }, [dataProvider, release, album.id])

  const paired = !!(release && match)
  // The release's part of an apply: its ID and what the admin ticked, once paired
  const releaseFields = paired ? { mbzAlbumId: release.id, ...options } : {}
  // Applies the paired release, if any, with the caller's own fields (the album's values, say)
  // as one edit job, and tells what came of it (see applyOutcome). A refused apply rejects with
  // the server's error, for the caller to say why.
  const apply = (fields) =>
    dataProvider
      .correctAlbum(album.id, { ...fields, ...releaseFields })
      .then(({ data }) => applyOutcome(data))

  return {
    album,
    release,
    pick,
    match,
    matchError,
    options,
    setOptions,
    // A picked release is applied once the admin has seen how it pairs
    paired,
    // Nothing is waited for: no release is picked, or its pairing is in (or couldn't be had,
    // and the release is left out)
    settled: !release || !!match || !!matchError,
    apply,
  }
}
