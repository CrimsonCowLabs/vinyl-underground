// Lets the app open downloaded Playlists with no network. Wraps the
// react-admin data provider: Playlist and Track reads that can't reach the
// server are answered from the Downloads instead. Server errors (a response
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
  // Returns the offline answer for a request, or undefined if there is none
  const fromDownloads = {
    getOne: (resource, { id }) => {
      const record = resource === 'playlist' && downloads.getRecord(id)
      return record ? { data: record.playlist } : undefined
    },
    getList: (resource, params) => {
      if (resource === 'playlist') {
        const playlists = downloads
          .list()
          .map(({ id }) => downloads.getRecord(id)?.playlist)
          .filter(Boolean)
        return sortAndPage(playlists, params)
      }
      if (resource === 'playlistTrack') {
        const record = downloads.getRecord(
          params.filter?.playlist_id ?? params.id,
        )
        return record ? sortAndPage(record.tracks, params) : undefined
      }
      return undefined
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
