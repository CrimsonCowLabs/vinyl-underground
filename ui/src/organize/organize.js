import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// What Organize (#167) works on, as the server takes it: an album ({ albumId }), the selected
// Tracks ({ ids }), or all matching the Library manager's filter ({ filter }, an empty one being
// the whole Library).
export const organizeScope = ({ albumId, ids, filter }) => {
  if (albumId) return { albumId }
  if (filter) return { filter }
  return { ids: ids || [] }
}

// The plan: { token, tracks, files, inPlace, unplaced: { reason: n }, moves: [...], left: [...],
// totalMoves, totalLeft }, with the moves and the Files left from start to end. Each row is
// { trackId, path, newPath, title, artist, album, status }.
export const planOrganize = (scope, start = 0, end = 100) =>
  httpClient(`${REST_URL}/organize/plan?_start=${start}&_end=${end}`, {
    method: 'POST',
    body: JSON.stringify(scope),
  }).then(({ json }) => json)

// Runs the plan reviewed (its token) as one edit job: { job, background }, background while the
// job runs on after the server stopped waiting for it (the Activity panel follows it).
export const runOrganize = (scope, token) =>
  httpClient(`${REST_URL}/organize/run`, {
    method: 'POST',
    body: JSON.stringify({ ...scope, token }),
  }).then(({ status, json }) => ({ job: json, background: status === 202 }))

// The reasons a File stays where it is, each explained on its own.
export const reasons = [
  'noAlbumArtist',
  'noAlbum',
  'noTitle',
  'titleNumber',
  'noTrackNumber',
  'noDiscNumber',
  'caseTwin',
  'samePath',
  'pathTaken',
  'editingOff',
]
