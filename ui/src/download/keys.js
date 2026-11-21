// Where Downloads live in Cache Storage. Shared by the app, which stores them,
// and the service worker, which serves them.
//
// Keys are synthetic, origin-absolute URLs keyed on ids, never on the URL the
// player requested: stream and cover art URLs carry a fresh auth token and
// salt on every request.

export const DOWNLOADS_CACHE = 'vu-downloads-v1'

const PREFIX = '/__downloads'

export const trackAudioKey = (trackId) =>
  `${PREFIX}/track/${encodeURIComponent(trackId)}`

export const coverArtKey = (coverArtId) =>
  `${PREFIX}/cover/${encodeURIComponent(coverArtId)}`

export const downloadRecordKey = (playlistId) =>
  `${PREFIX}/playlist/${encodeURIComponent(playlistId)}`

export const DOWNLOAD_RECORD_PREFIX = `${PREFIX}/playlist/`
export const TRACK_AUDIO_PREFIX = `${PREFIX}/track/`

const subsonicCommand = (url) => {
  const match = url.pathname.match(/\/rest\/(\w+?)(\.view)?$/)
  return match ? match[1] : null
}

// The Track id of a Subsonic stream request, or null if it isn't one
export const audioTrackId = (url) => {
  switch (subsonicCommand(url)) {
    case 'stream':
      return url.searchParams.get('id')
    case 'getTranscodeStream':
      return url.searchParams.get('mediaId')
    default:
      return null
  }
}

// The artwork id of a getCoverArt request, without its version suffix
// (`mf-<id>_<hash>`), so every version and size maps to one stored cover
export const coverArtId = (url) => {
  if (subsonicCommand(url) !== 'getCoverArt') return null
  const id = url.searchParams.get('id')
  return id ? id.split('_')[0] : null
}
