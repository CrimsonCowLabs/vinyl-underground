import jsonServerProvider from 'ra-data-json-server'
import httpClient, { fetchImage } from './httpClient'
import { REST_URL } from '../consts'

const dataProvider = jsonServerProvider(REST_URL, httpClient)

const REFRESH_KIND = { album: 'al', artist: 'ar' }

// writeResult is what a save writing to the Files answers (an edit job the server waits for a
// little): { jobId, done, job, albumId, artistId, pickCover } once done, with the Files the job
// skipped in job.steps; or { jobId, background: true } while it carries on.
const writeResult = ({ json }) => ({ data: { id: json.jobId, ...json } })

const isAdmin = () => {
  const role = localStorage.getItem('role')
  return role === 'admin'
}

const getSelectedLibraries = () => {
  try {
    const state = JSON.parse(localStorage.getItem('state'))
    const selectedLibraries = state?.library?.selectedLibraries || []
    const userLibraries = state?.library?.userLibraries || []

    // Validate selected libraries against current user libraries
    const userLibraryIds = userLibraries.map((lib) => lib.id)
    const validatedSelection = selectedLibraries.filter((id) =>
      userLibraryIds.includes(id),
    )

    // If user has only one library, return empty array (no filter needed)
    if (userLibraryIds.length === 1) {
      return []
    }

    return validatedSelection
  } catch (err) {
    return []
  }
}

// Function to apply library filtering to appropriate resources
const applyLibraryFilter = (resource, params) => {
  // Content resources that should be filtered by selected libraries
  const filteredResources = ['album', 'song', 'artist', 'playlistTrack', 'tag']

  // Get selected libraries from localStorage
  const selectedLibraries = getSelectedLibraries()

  // Add library filter for content resources if libraries are selected
  if (filteredResources.includes(resource) && selectedLibraries.length > 0) {
    if (!params.filter) {
      params.filter = {}
    }
    params.filter.library_id = selectedLibraries
  }

  return params
}

const mapResource = (resource, params) => {
  switch (resource) {
    // /api/playlistTrack?playlist_id=123  => /api/playlist/123/tracks
    case 'playlistTrack': {
      params.filter = params.filter || {}

      let plsId = '0'
      plsId = params.filter.playlist_id
      if (!isAdmin()) {
        params.filter.missing = false
      }
      params = applyLibraryFilter(resource, params)

      return [`playlist/${plsId}/tracks`, params]
    }
    case 'album':
    case 'song':
    case 'artist':
    case 'tag': {
      params.filter = params.filter || {}
      if (!isAdmin()) {
        params.filter.missing = false
      }
      params = applyLibraryFilter(resource, params)

      return [resource, params]
    }
    default:
      return [resource, params]
  }
}

const callDeleteMany = (resource, params) => {
  const ids = (params.ids || []).map((id) => `id=${id}`)
  const query = ids.length > 0 ? `?${ids.join('&')}` : ''
  return httpClient(`${REST_URL}/${resource}${query}`, {
    method: 'DELETE',
  }).then((response) => ({ data: response.json.ids || [] }))
}

// Helper function to handle user-library associations
const handleUserLibraryAssociation = async (userId, libraryIds) => {
  if (!libraryIds || libraryIds.length === 0) {
    return // Admin users or users without library assignments
  }

  try {
    await httpClient(`${REST_URL}/user/${userId}/library`, {
      method: 'PUT',
      body: JSON.stringify({ libraryIds }),
    })
  } catch (error) {
    console.error('Error setting user libraries:', error) //eslint-disable-line no-console
    throw error
  }
}

// Enhanced user creation that handles library associations
const createUser = async (params) => {
  const { data } = params
  const { libraryIds, ...userData } = data

  // First create the user
  const userResponse = await dataProvider.create('user', { data: userData })
  const userId = userResponse.data.id

  // Then set library associations for non-admin users
  if (!userData.isAdmin && libraryIds && libraryIds.length > 0) {
    await handleUserLibraryAssociation(userId, libraryIds)
  }

  return userResponse
}

// Enhanced user update that handles library associations
const updateUser = async (params) => {
  const { data } = params
  const { libraryIds, ...userData } = data
  const userId = params.id

  // First update the user
  const userResponse = await dataProvider.update('user', {
    ...params,
    data: userData,
  })

  // Then handle library associations for non-admin users. Only admins can call
  // this endpoint; for self-edits the server manages library assignments
  if (isAdmin() && !userData.isAdmin && libraryIds !== undefined) {
    await handleUserLibraryAssociation(userId, libraryIds)
  }

  return userResponse
}

const wrapperDataProvider = {
  ...dataProvider,
  getList: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    return dataProvider.getList(r, p)
  },
  getOne: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    const response = dataProvider.getOne(r, p)

    // Transform user data to ensure libraryIds is present for form compatibility
    if (resource === 'user') {
      return response.then((result) => {
        if (result.data.libraries && Array.isArray(result.data.libraries)) {
          result.data.libraryIds = result.data.libraries.map((lib) => lib.id)
        }
        return result
      })
    }

    return response
  },
  getMany: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    return dataProvider.getMany(r, p)
  },
  getManyReference: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    return dataProvider.getManyReference(r, p)
  },
  update: (resource, params) => {
    if (resource === 'user') {
      return updateUser(params)
    }
    const [r, p] = mapResource(resource, params)
    return dataProvider.update(r, p)
  },
  updateMany: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    return dataProvider.updateMany(r, p)
  },
  create: (resource, params) => {
    if (resource === 'user') {
      return createUser(params)
    }
    const [r, p] = mapResource(resource, params)
    return dataProvider.create(r, p)
  },
  delete: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    return dataProvider.delete(r, p)
  },
  deleteMany: (resource, params) => {
    const [r, p] = mapResource(resource, params)
    if (r.endsWith('/tracks') || resource === 'missing') {
      return callDeleteMany(r, p)
    }
    return dataProvider.deleteMany(r, p)
  },
  addToPlaylist: (playlistId, data) => {
    return httpClient(`${REST_URL}/playlist/${playlistId}/tracks`, {
      method: 'POST',
      body: JSON.stringify(data),
    }).then(({ json }) => ({ data: json }))
  },
  getPlaylists: (songId) => {
    return httpClient(`${REST_URL}/song/${songId}/playlists`).then(
      ({ json }) => ({ data: json }),
    )
  },
  lookupQuickConnect: (code) =>
    httpClient(
      `${REST_URL}/quickconnect?code=${encodeURIComponent(code)}`,
    ).then(({ json }) => ({ data: json })),
  authorizeQuickConnect: (code) =>
    httpClient(`${REST_URL}/quickconnect/authorize`, {
      method: 'POST',
      body: JSON.stringify({ code }),
    }).then(({ json }) => ({ data: json })),
  getStreamingQuality: () =>
    httpClient(`${REST_URL}/streamingQuality`).then(({ json }) => ({
      data: json,
    })),
  setStreamingQuality: (preference) =>
    httpClient(`${REST_URL}/streamingQuality`, {
      method: 'PUT',
      body: JSON.stringify({ preference }),
    }).then(({ json }) => ({ data: json })),
  inspect: (songId) => {
    return httpClient(`${REST_URL}/inspect?id=${songId}`).then(({ json }) => ({
      data: json,
    }))
  },
  // The endpoint answers 204 with no body, but react-admin rejects any response without a
  // `data` key, so the id stands in for one.
  refreshMetadata: (resource, id) =>
    httpClient(`${REST_URL}/metadata/${REFRESH_KIND[resource]}/${id}/refresh`, {
      method: 'POST',
    }).then(() => ({ data: { id } })),
  // Artist rename and merge (admin only) write the new name into the Artist's Files, as one edit
  // job (see writeResult), answering the Artist's new ID (artistId). A failure's body has an
  // `error` code to translate.
  renameArtist: (artistId, name) =>
    httpClient(`${REST_URL}/correction/artistRename`, {
      method: 'POST',
      body: JSON.stringify({ artistId, name }),
    }).then(writeResult),
  // The cover picker (admin only): one source's candidates, { candidates, failed }, and choosing
  // one, which saves an album cover Correction.
  getCoverCandidates: (albumId, source) =>
    httpClient(
      `${REST_URL}/albumCover/${albumId}/candidates?source=${encodeURIComponent(source)}`,
    ).then(({ json }) => ({ data: json })),
  // A candidate's preview, { data: Blob } (see fetchImage). The signal travels in the payload,
  // as react-admin's useDataProvider passes on only (resource, payload).
  getCoverPreview: (albumId, { candidateId, signal }) =>
    fetchImage(
      `${REST_URL}/albumCover/${albumId}/candidates/${candidateId}/preview`,
      { signal },
    ).then((blob) => ({ data: blob })),
  // Choosing a candidate writes it as the album folder's cover.jpg (#164), as an edit job:
  // { jobId, changed, entryId, warning }, or { jobId, background: true } while it runs on.
  chooseAlbumCover: (albumId, candidateId) =>
    httpClient(`${REST_URL}/correction/albumCover`, {
      method: 'POST',
      body: JSON.stringify({ albumId, candidateId }),
    }).then(({ json }) => ({ data: { id: json.jobId, ...json } })),
  // Uploading an image writes it as the album's cover, like a chosen candidate. Marking the
  // cover wrong blocks the image the album shows (its imageHash, as the admin saw it).
  uploadAlbumCover: (albumId, file) => {
    const body = new FormData()
    body.append('albumId', albumId)
    body.append('image', file)
    return httpClient(`${REST_URL}/correction/albumCoverUpload`, {
      method: 'POST',
      body,
    }).then(({ json }) => ({ data: { id: json.jobId, ...json } }))
  },
  // Embed in Files: the album folder's cover into each of its Files, as one edit job, { id,
  // state, total, done, skipped, steps } with its skipped Files, still running unless state is
  // 'done'.
  embedAlbumCover: (albumId) =>
    httpClient(`${REST_URL}/albumCover/${albumId}/embed`, {
      method: 'POST',
    }).then(({ json }) => ({ data: json })),
  blockAlbumCover: (albumId, imageHash) =>
    httpClient(`${REST_URL}/correction/albumCoverBlock`, {
      method: 'POST',
      body: JSON.stringify({ albumId, imageHash }),
    }).then(({ json }) => ({ data: { id: json.correction.id, ...json } })),
  mergeArtist: (artistId, intoId) =>
    httpClient(`${REST_URL}/correction/artistMerge`, {
      method: 'POST',
      body: JSON.stringify({ artistId, intoId }),
    }).then(writeResult),
  // Writes the album's title, album artist and year, and its Tracks' changed numbers (tracks, if
  // any), into its Files, as one edit job (see writeResult). With a MusicBrainz release picked
  // with Find on MusicBrainz (mbzAlbumId), the job writes its IDs too, and the release's values
  // the admin accepts (canonical: { album, albumArtist, date, titles, artists }), replacing track IDs that
  // differ only with replaceTrackIds, its cover as cover.jpg with cover, and the album's
  // MusicBrainz genres in place of the Files' with genres. The answer then has
  // pickCover, and rolledBack when a File couldn't be written, so the others were put back.
  correctAlbum: (albumId, { name, albumArtist, year, tracks, ...mbz }) =>
    httpClient(`${REST_URL}/correction/album`, {
      method: 'POST',
      body: JSON.stringify({
        albumId,
        name,
        albumArtist,
        year,
        tracks,
        ...mbz,
      }),
    }).then(writeResult),
  // Writes the disc and track numbers of the album's Tracks, [{ id, discNumber, trackNumber }],
  // into their Files, as one edit job (see writeResult).
  correctTrackNumbers: (albumId, tracks) =>
    httpClient(`${REST_URL}/correction/trackNumbers`, {
      method: 'POST',
      body: JSON.stringify({ albumId, tracks }),
    }).then(writeResult),
  // A Track's tags as its File holds them (admin only, while editing is on): { id, path,
  // fields: [{ key, type, multi }], values: { key: [values] } }. A failure's body has an `error`
  // code to translate.
  getTrackTags: (id) =>
    httpClient(`${REST_URL}/trackTags/${id}`).then(({ json }) => ({
      data: json,
    })),
  // Writes the changed fields (by key; no values removes a tag) into the Track's File, as an
  // edit job the server waits for a little: { changed, track, warning, jobId }, or
  // { jobId, background: true } when the job carries on in the background.
  editTrackTags: (id, values) =>
    httpClient(`${REST_URL}/trackTags/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ values }),
    }).then(({ json }) => ({ data: json })),
  // Find on MusicBrainz (admin only): the MusicBrainz releases matching what the admin typed
  // (query: a title, searched with the album artist, or a barcode, catalog number or release
  // link, which the server tells apart), [{ id, releaseGroupId, title, artist, year, country,
  // format, status, score, trackCount, description, thumbnailUrl }]. A compilation is searched
  // as Various Artists. A failure's body has an `error` code to translate.
  findMusicBrainzReleases: (artist, query, compilation) =>
    httpClient(
      `${REST_URL}/musicbrainz/release?artist=${encodeURIComponent(artist)}&q=${encodeURIComponent(query)}${compilation ? '&compilation=true' : ''}`,
    ).then(({ json }) => ({ data: json })),
  // How the album's Tracks pair with a MusicBrainz release, before saving it (#170): { release,
  // holds, tracks: [{ id, title, disc, track, paired: { id, recordingId, title, disc, position },
  // mismatch }], unpaired, albumArtistMatches, canonical: { album, albumArtist, date: { from,
  // to }, titles } }. A failure's body has an `error` code to translate.
  matchMusicBrainzRelease: (releaseId, albumId) =>
    httpClient(
      `${REST_URL}/musicbrainz/release/${encodeURIComponent(releaseId)}/match?albumId=${encodeURIComponent(albumId)}`,
    ).then(({ json }) => ({ data: json })),
  // Which of the MusicBrainz releases (IDs in search order; the server looks up five at most)
  // fits the album's Files best: [{ id, paired, trackCount, albumTracks, lengthDiff (seconds, or
  // null), best }], leaving out those it couldn't look up. A failure's body has an `error` code.
  fitMusicBrainzReleases: (albumId, releaseIds) =>
    httpClient(
      `${REST_URL}/musicbrainz/release/fit?albumId=${encodeURIComponent(albumId)}&ids=${encodeURIComponent(releaseIds.join(','))}`,
    ).then(({ json }) => ({ data: json })),
  // The Correction migration (#165, admin only): the plan, { items: [{ correction, changes:
  // [{ field, from, to }], files, flags: [{ path, code, error }], lastRun }],
  // files }, which writes nothing; and approving it, which writes them all into the Files as one
  // edit job (see writeResult), or answers { done: true } alone with nothing to write.
  getCorrectionMigration: () =>
    httpClient(`${REST_URL}/correction/migration`).then(({ json }) => ({
      data: json,
    })),
  migrateCorrections: () =>
    httpClient(`${REST_URL}/correction/migration`, { method: 'POST' }).then(
      writeResult,
    ),
  // Undoes a cover block; the Corrections the migration writes can't be undone (migrateFirst).
  undoCorrection: (id) =>
    httpClient(`${REST_URL}/correction/${id}/undo`, { method: 'POST' }).then(
      ({ json }) => ({ data: { id, ...json } }),
    ),
  // Undoes an Edit journal entry (admin only), as an edit job of one File: writes the File's
  // earlier values back, journaled as a new entry: { changed, track, entryId, warning, jobId },
  // or { jobId, background: true } while the job carries on. A refusal's body has an `error` code.
  undoEdit: (id) =>
    httpClient(`${REST_URL}/editHistory/${id}/undo`, { method: 'POST' }).then(
      ({ json }) => ({ data: json }),
    ),
  // Undoes a whole edit job (admin only), as a job of its own: the undo job, { id, state, total,
  // done, skipped, steps } with its skipped Files, still running unless state is 'done'.
  undoEditJob: (jobId) =>
    httpClient(`${REST_URL}/editJob/${jobId}/undo`, { method: 'POST' }).then(
      ({ json }) => ({ data: json }),
    ),
}

export default wrapperDataProvider
