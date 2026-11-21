// Keeps track of a Listener's Downloads: which Playlists and albums are
// stored on the device, how far each one has got, and which Tracks are safe
// to delete when a Download is removed.
//
// The browser APIs live behind three seams so this logic can be tested:
//   server.getPlaylist(id) -> { playlist, tracks }
//   server.getPlaylistInfo(id) -> playlist, or null if it no longer exists
//   server.getAlbum(id) -> { album, tracks }
//   storage: listDownloads, saveDownload, deleteDownload(id, kind),
//            listTrackIds, listTrackSizes() -> { [trackId]: bytes },
//            saveTrack(track, { signal }), deleteTrack(trackId)
//   requestPersistence() -> asks the browser not to evict the Downloads
//
// A Download is a Playlist or an album (its kind). Its stored record is
// { listener, kind, [kind]: playlist or album, tracks }; records stored
// before albums could be downloaded have no kind and are Playlists.
//
// Tracks are stored one at a time. A Track that fails to store leaves its
// Download interrupted; resume() retries whatever is still missing.
//
// refresh() brings Downloads up to date with Playlists changed elsewhere:
// added Tracks are stored, removed ones deleted, and the new order kept.

export const PLAYLIST = 'playlist'
export const ALBUM = 'album'

export const trackIdOf = (track) => track.mediaFileId || track.id

export const kindOf = (record) => record.kind || PLAYLIST

// The Playlist or album a Download record holds
export const itemOf = (record) => record[kindOf(record)]

const keyOf = (id, kind) => `${kind}:${id}`

const fetchers = {
  [PLAYLIST]: (server, id) => server.getPlaylist(id),
  [ALBUM]: (server, id) => server.getAlbum(id),
}

export const createDownloads = ({
  server,
  storage,
  requestPersistence,
  currentListener,
}) => {
  const records = new Map() // `${kind}:${id}` -> record
  const stored = new Set() // Track ids stored on the device
  const failed = new Set() // Track ids that failed during the current run
  const listeners = new Set()
  let running = null
  let refreshing = null
  let inFlight = null // { trackId, controller }

  const notify = () => listeners.forEach((fn) => fn())

  const mine = (id, kind = PLAYLIST) => {
    const record = records.get(keyOf(id, kind))
    return record && record.listener === currentListener() ? record : undefined
  }

  const myRecords = () =>
    [...records.values()].filter((r) => r.listener === currentListener())

  const trackIdsOf = (record) => [...new Set(record.tracks.map(trackIdOf))]

  const isReferenced = (trackId) =>
    [...records.values()].some((r) =>
      r.tracks.some((t) => trackIdOf(t) === trackId),
    )

  // Deletes those of the given Tracks that no Download uses any more
  const deleteOrphans = async (trackIds) => {
    const orphans = trackIds.filter((tid) => !isReferenced(tid))
    if (inFlight && orphans.includes(inFlight.trackId)) {
      inFlight.controller.abort()
    }
    orphans.forEach((tid) => stored.delete(tid))
    notify()
    await Promise.all(orphans.map((tid) => storage.deleteTrack(tid)))
  }

  const nextTrack = () => {
    for (const record of records.values()) {
      if (record.listener !== currentListener()) continue
      const t = record.tracks.find((t) => {
        const id = trackIdOf(t)
        return !stored.has(id) && !failed.has(id)
      })
      if (t) return t
    }
    return undefined
  }

  const run = async () => {
    let t
    while ((t = nextTrack())) {
      const trackId = trackIdOf(t)
      const controller = new AbortController()
      inFlight = { trackId, controller }
      try {
        await storage.saveTrack(t, { signal: controller.signal })
        if (isReferenced(trackId)) {
          stored.add(trackId)
        } else {
          // Its Download was removed while the Track was on its way
          await storage.deleteTrack(trackId)
        }
      } catch {
        failed.add(trackId)
      }
      inFlight = null
      notify()
    }
  }

  // Runs the queue until nothing is left; concurrent callers share one run.
  const drain = () => {
    if (!running) {
      running = run().finally(() => {
        running = null
        notify()
      })
      notify()
    }
    return running
  }

  const get = (id, kind = PLAYLIST) => {
    const record = mine(id, kind)
    if (!record) return undefined
    const ids = trackIdsOf(record)
    const done = ids.filter((tid) => stored.has(tid)).length
    const pending = ids.some((tid) => !stored.has(tid) && !failed.has(tid))
    let state = 'interrupted'
    if (done === ids.length) state = 'downloaded'
    // A Download counts as downloading while a refresh checks every
    // Playlist, before it stores the Tracks it finds missing
    else if ((running || refreshing) && pending) state = 'downloading'
    const { name } = itemOf(record)
    return { id, kind, name, state, done, total: ids.length }
  }

  const remove = async (id, kind = PLAYLIST) => {
    const record = mine(id, kind)
    if (!record) return
    records.delete(keyOf(id, kind))
    await storage.deleteDownload(id, kind)
    await deleteOrphans(trackIdsOf(record))
  }

  // A Smart Playlist's Tracks change when the server re-evaluates its rules,
  // which doesn't touch its updatedAt
  const hasChanged = (before, now) =>
    Boolean(now.rules) || now.updatedAt !== before.updatedAt

  // Only Playlist Downloads are refreshed: an album doesn't change (ADR-0006)
  const refreshOne = async (id) => {
    const key = keyOf(id, PLAYLIST)
    const before = records.get(key)
    if (!before) return
    const info = await server.getPlaylistInfo(id)
    // Each await below may race a remove or start of the same Download
    const replaced = () => records.get(key) !== before
    if (replaced()) return
    if (!info) return remove(id) // deleted on the server
    if (!hasChanged(before.playlist, info)) return
    const { playlist, tracks } = await server.getPlaylist(id)
    if (replaced()) return
    const record = {
      listener: before.listener,
      kind: PLAYLIST,
      playlist,
      tracks,
    }
    records.set(key, record)
    await storage.saveDownload(record)
    // Removed meanwhile
    if (!records.has(key)) await storage.deleteDownload(id, PLAYLIST)
    await deleteOrphans(trackIdsOf(before))
  }

  const refreshAll = async () => {
    const playlistIds = myRecords()
      .filter((r) => kindOf(r) === PLAYLIST)
      .map((r) => itemOf(r).id)
    for (const id of playlistIds) {
      // Unreachable now: the Download stays as it is until the next refresh
      await refreshOne(id).catch(() => {})
    }
  }

  // Brings the current Listener's Downloads up to date with their Playlists,
  // then stores whatever is missing, including Tracks that failed before.
  // Concurrent callers share one refresh.
  const refresh = () => {
    if (!refreshing) {
      refreshing = refreshAll().finally(() => {
        refreshing = null
      })
      // Downloads with Tracks still to store now count as downloading
      notify()
    }
    return refreshing.then(() => {
      failed.clear()
      return drain()
    })
  }

  return {
    load: async () => {
      const [list, trackIds] = await Promise.all([
        storage.listDownloads(),
        storage.listTrackIds(),
      ])
      list.forEach((r) => records.set(keyOf(itemOf(r).id, kindOf(r)), r))
      trackIds.forEach((id) => stored.add(id))
      notify()
    },

    start: async (id, kind = PLAYLIST) => {
      Promise.resolve(requestPersistence()).catch(() => {})
      const { tracks, [kind]: item } = await fetchers[kind](server, id)
      const record = { listener: currentListener(), kind, [kind]: item, tracks }
      await storage.saveDownload(record)
      records.set(keyOf(id, kind), record)
      record.tracks.forEach((t) => failed.delete(trackIdOf(t)))
      notify()
      return drain()
    },

    refresh,

    resume: () => {
      failed.clear()
      return drain()
    },

    remove,

    get,
    list: () => myRecords().map((r) => get(itemOf(r).id, kindOf(r))),

    // The space the current Listener's Downloads use on the device. A Track
    // shared by several Downloads counts in each of their sizes, as each
    // needs it, but only once in the total: the per-Download sizes can add up
    // to more than the total. Cover art is small and not counted.
    usage: async () => {
      const sizes = await storage.listTrackSizes()
      const sizeOf = (ids) =>
        ids.reduce((sum, tid) => sum + (sizes[tid] || 0), 0)
      const mineList = myRecords()
      return {
        total: sizeOf([...new Set(mineList.flatMap(trackIdsOf))]),
        downloads: mineList.map((r) => ({
          id: itemOf(r).id,
          kind: kindOf(r),
          name: itemOf(r).name,
          size: sizeOf(trackIdsOf(r)),
        })),
      }
    },

    getRecord: (id, kind = PLAYLIST) => mine(id, kind),

    // Every Track stored for the current Listener, once each, in Download
    // order: what "Shuffle all Downloads" plays
    downloadedTracks: () => {
      const seen = new Set()
      return myRecords()
        .flatMap((r) => r.tracks)
        .filter((t) => {
          const tid = trackIdOf(t)
          if (seen.has(tid) || !stored.has(tid)) return false
          seen.add(tid)
          return true
        })
    },
    isTrackDownloaded: (trackId) => stored.has(trackId),
    subscribe: (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
  }
}
