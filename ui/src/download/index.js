import { useEffect, useState } from 'react'
import dataProvider from '../dataProvider/wrapperDataProvider'
import { PLAYLIST, createDownloads, trackIdOf } from './downloads'
import { browserStorage } from './browserStorage'
import { withDownloads } from './offlineDataProvider'
import { createReachability } from './offline'
import { baseUrl } from '../utils'

// A missing Track has no File to stream, so it can't be downloaded
const playable = (tracks) => tracks.filter((t) => !t.missing)

const server = {
  getPlaylist: async (id) => {
    const [{ data: playlist }, { data: tracks }] = await Promise.all([
      dataProvider.getOne('playlist', { id }),
      dataProvider.getList('playlistTrack', {
        pagination: { page: 1, perPage: 0 },
        sort: { field: 'id', order: 'ASC' },
        filter: { playlist_id: id },
      }),
    ])
    return { playlist, tracks: playable(tracks) }
  },
  // The Tracks in the order the album page shows them
  getAlbum: async (id) => {
    const [{ data: album }, { data: tracks }] = await Promise.all([
      dataProvider.getOne('album', { id }),
      dataProvider.getList('song', {
        pagination: { page: 1, perPage: 0 },
        sort: { field: 'album', order: 'ASC' },
        filter: { album_id: id },
      }),
    ])
    return { album, tracks: playable(tracks) }
  },
  // Its updatedAt changes whenever its Tracks are added, removed or reordered
  getPlaylistInfo: async (id) => {
    try {
      const { data } = await dataProvider.getOne('playlist', { id })
      return data
    } catch (error) {
      if (error?.status === 404) return null
      throw error
    }
  },
}

// Without persistent storage, Android may evict Downloads when space is low
const requestPersistence = () =>
  navigator.storage?.persist ? navigator.storage.persist() : false

export const isOnline = () => navigator.onLine !== false

// Whether the server answers, which the data provider keeps up to date
export const serverReachability = createReachability({
  probe: () =>
    fetch(baseUrl('/ping'), { cache: 'no-store' }).then((res) => res.ok),
})

export const downloads = createDownloads({
  server,
  storage: browserStorage,
  requestPersistence,
  currentListener: () => localStorage.getItem('username'),
})

const supported = () => typeof caches !== 'undefined'

let loaded = null
const loadOnce = () => {
  if (!loaded) loaded = supported() ? downloads.load() : Promise.resolve()
  return loaded
}

export const offlineAware = (provider) =>
  withDownloads(provider, {
    downloads,
    isOnline,
    ready: loadOnce,
    reachability: serverReachability,
  })

// Picks up Downloads stored earlier, brings them up to date with their
// Playlists and resumes any that were interrupted: now, whenever the device
// comes back online, and whenever the app is brought back to the screen.
export const useResumeDownloads = () => {
  useEffect(() => {
    if (!supported()) return undefined
    const refresh = () => {
      if (isOnline()) downloads.refresh()
    }
    const refreshIfVisible = () => {
      if (document.visibilityState === 'visible') refresh()
    }
    loadOnce().then(refresh, () => {})
    window.addEventListener('online', refresh)
    document.addEventListener('visibilitychange', refreshIfVisible)
    return () => {
      window.removeEventListener('online', refresh)
      document.removeEventListener('visibilitychange', refreshIfVisible)
    }
  }, [])
}

// Something worked out from the Downloads, kept up to date
const useDownloadsState = (read, deps) => {
  const [value, setValue] = useState(read)
  useEffect(() => {
    const update = () => setValue(read())
    update()
    loadOnce().then(update, () => {})
    return downloads.subscribe(update)
  }, deps) // eslint-disable-line react-hooks/exhaustive-deps
  return value
}

// The current Listener's Download of a Playlist or album, kept up to date
export const useDownload = (id, kind = PLAYLIST) =>
  useDownloadsState(() => downloads.get(id, kind), [id, kind])

// All of the current Listener's Downloads, kept up to date
export const useDownloadList = () =>
  useDownloadsState(() => downloads.list(), [])

// What the browser reports for the whole app (Downloads, app shell and
// anything else it stores), or undefined if it can't tell
const estimateStorage = async () => {
  try {
    const { usage, quota } = (await navigator.storage?.estimate?.()) || {}
    return quota ? { used: usage || 0, quota } : undefined
  } catch {
    return undefined
  }
}

// The space the current Listener's Downloads use, for the storage meter:
// { total, downloads: [{ id, kind, name, size }], device: { used, quota } }.
// Undefined until first measured; measured again as Downloads change.
export const useDownloadUsage = () => {
  const [usage, setUsage] = useState()
  useEffect(() => {
    if (!supported()) return undefined
    let active = true
    let measuring = false
    let again = false
    const measure = async () => {
      // While a Download is storing Tracks, measure at most once at a time
      if (measuring) {
        again = true
        return
      }
      measuring = true
      try {
        const [mine, device] = await Promise.all([
          downloads.usage(),
          estimateStorage(),
        ])
        if (active) setUsage({ ...mine, device })
      } catch {
        // keep the last measurement
      }
      measuring = false
      if (again && active) {
        again = false
        measure()
      }
    }
    loadOnce().then(measure, () => {})
    const unsubscribe = downloads.subscribe(measure)
    return () => {
      active = false
      unsubscribe()
    }
  }, [])
  return usage
}

// Every downloaded Track once, keyed by Track id, ready for the player's
// shuffleTracks action: what "Shuffle all Downloads" plays
export const downloadedTracksForPlayer = async () => {
  await loadOnce().catch(() => {})
  return Object.fromEntries(
    downloads.downloadedTracks().map((t) => [trackIdOf(t), t]),
  )
}

const offlineNow = () => !isOnline() || !serverReachability.isReachable()

// Whether the device has lost its network connection or the server doesn't
// answer, kept up to date. Anything that needs the server (e.g. Stations,
// Generated Playlists) can use it to show itself as unavailable offline
// rather than fail.
export const useIsOffline = () => {
  const [offline, setOffline] = useState(offlineNow)
  useEffect(() => {
    const update = () => setOffline(offlineNow())
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    const unsubscribe = serverReachability.subscribe(update)
    return () => {
      window.removeEventListener('online', update)
      window.removeEventListener('offline', update)
      unsubscribe()
    }
  }, [])
  return offline
}

export { isAvailableOffline } from './offline'

export const downloadsSupported = supported
