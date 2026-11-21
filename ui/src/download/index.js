import { useEffect, useState } from 'react'
import dataProvider from '../dataProvider/wrapperDataProvider'
import { PLAYLIST, createDownloads } from './downloads'
import { browserStorage } from './browserStorage'
import { withDownloads } from './offlineDataProvider'

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
  withDownloads(provider, { downloads, isOnline, ready: loadOnce })

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

// Whether the device has lost its network connection, kept up to date.
// Anything that needs the server (e.g. Stations, Generated Playlists) can use
// it to show itself as unavailable offline rather than fail.
export const useIsOffline = () => {
  const [offline, setOffline] = useState(() => !isOnline())
  useEffect(() => {
    const update = () => setOffline(!isOnline())
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => {
      window.removeEventListener('online', update)
      window.removeEventListener('offline', update)
    }
  }, [])
  return offline
}

export const downloadsSupported = supported
