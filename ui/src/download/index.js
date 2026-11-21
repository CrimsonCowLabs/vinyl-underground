import { useEffect, useState } from 'react'
import dataProvider from '../dataProvider/wrapperDataProvider'
import { createDownloads } from './downloads'
import { browserStorage } from './browserStorage'
import { withDownloads } from './offlineDataProvider'

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
    // A missing Track has no File to stream, so it can't be downloaded
    return { playlist, tracks: tracks.filter((t) => !t.missing) }
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

// Picks up Downloads stored earlier, and resumes any that were interrupted,
// now and whenever the device comes back online.
export const useResumeDownloads = () => {
  useEffect(() => {
    if (!supported()) return undefined
    const resume = () => {
      if (isOnline()) downloads.resume()
    }
    loadOnce().then(resume, () => {})
    window.addEventListener('online', resume)
    return () => window.removeEventListener('online', resume)
  }, [])
}

// The current Listener's Download of a Playlist, kept up to date
export const useDownload = (playlistId) => {
  const [download, setDownload] = useState(() => downloads.get(playlistId))
  useEffect(() => {
    const update = () => setDownload(downloads.get(playlistId))
    update()
    loadOnce().then(update, () => {})
    return downloads.subscribe(update)
  }, [playlistId])
  return download
}

export const downloadsSupported = supported
