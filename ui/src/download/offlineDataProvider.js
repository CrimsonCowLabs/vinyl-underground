// Lets the app open downloaded Playlists and albums with no network. Wraps
// the react-admin data provider: Playlist, album and Track reads that can't
// reach the server are answered from the Downloads instead. Server errors (a response
// with a status) pass through untouched.

const isNetworkError = (error) => error && error.status === undefined

const compare = (field, order) => (a, b) => {
  const x = a[field]
  const y = b[field]
  let result
  if (typeof x === 'number' && typeof y === 'number') {
    result = x - y
  } else {
    result = String(x ?? '').localeCompare(String(y ?? ''), undefined, {
      numeric: true,
      sensitivity: 'base',
    })
  }
  return order === 'DESC' ? -result : result
}

const sortAndPage = (records, { sort, pagination } = {}) => {
  const sorted = sort?.field
    ? [...records].sort(compare(sort.field, sort.order))
    : records
  const { page = 1, perPage = 0 } = pagination || {}
  const data =
    perPage > 0 ? sorted.slice((page - 1) * perPage, page * perPage) : sorted
  return { data, total: records.length }
}

// `ready()` resolves once the Downloads have been read from the device, which
// happens asynchronously as the app starts.
export const withDownloads = (
  dataProvider,
  { downloads, isOnline, ready = () => Promise.resolve() },
) => {
  // The Downloads' Playlists or albums
  const items = (kind) =>
    downloads
      .list()
      .filter((d) => d.kind === kind)
      .map(({ id }) => downloads.getRecord(id, kind)?.[kind])
      .filter(Boolean)

  // The Tracks of one Download
  const tracksOf = (kind, id, params) => {
    const record = id != null && downloads.getRecord(id, kind)
    return record ? sortAndPage(record.tracks, params) : undefined
  }

  // Returns the offline answer for a request, or undefined if there is none
  const fromDownloads = {
    getOne: (resource, { id }) => {
      const kind = ['playlist', 'album'].includes(resource) && resource
      const record = kind && downloads.getRecord(id, kind)
      return record ? { data: record[kind] } : undefined
    },
    getList: (resource, params) => {
      switch (resource) {
        case 'playlist':
        case 'album':
          return sortAndPage(items(resource), params)
        case 'playlistTrack':
          return tracksOf(
            'playlist',
            params.filter?.playlist_id ?? params.id,
            params,
          )
        case 'song':
          // Only the Tracks of one album, as the album page asks for them
          return tracksOf(
            'album',
            params.filter?.album_id ??
              (params.target === 'album_id' ? params.id : undefined),
            params,
          )
        default:
          return undefined
      }
    },
  }
  fromDownloads.getManyReference = fromDownloads.getList

  const withFallback = (method) => async (resource, params) => {
    const offline = async () => {
      await ready().catch(() => {})
      return fromDownloads[method](resource, params)
    }
    if (!isOnline()) {
      const answer = await offline()
      if (answer) return answer
    }
    try {
      return await dataProvider[method](resource, params)
    } catch (error) {
      const answer = isNetworkError(error) && (await offline())
      if (answer) return answer
      throw error
    }
  }

  return {
    ...dataProvider,
    getOne: withFallback('getOne'),
    getList: withFallback('getList'),
    getManyReference: withFallback('getManyReference'),
  }
}
