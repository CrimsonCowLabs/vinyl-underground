// Keeps track of a Listener's Downloads: which Playlists are stored on the
// device, how far each one has got, and which Tracks are safe to delete when
// a Download is removed.
//
// The browser APIs live behind three seams so this logic can be tested:
//   server.getPlaylist(id) -> { playlist, tracks }
//   server.getPlaylistInfo(id) -> playlist, or null if it no longer exists
//   storage: listDownloads, saveDownload, deleteDownload, listTrackIds,
//            saveTrack(track, { signal }), deleteTrack(trackId)
//   requestPersistence() -> asks the browser not to evict the Downloads
//
// Tracks are stored one at a time. A Track that fails to store leaves its
// Download interrupted; resume() retries whatever is still missing.
//
// refresh() brings Downloads up to date with Playlists changed elsewhere:
// added Tracks are stored, removed ones deleted, and the new order kept.

export const trackIdOf = (track) => track.mediaFileId || track.id

export const createDownloads = ({
  server,
  storage,
  requestPersistence,
  currentListener,
}) => {
  const records = new Map() // playlist id -> { listener, playlist, tracks }
  const stored = new Set() // Track ids stored on the device
  const failed = new Set() // Track ids that failed during the current run
  const listeners = new Set()
  let running = null
  let refreshing = null
  let inFlight = null // { trackId, controller }

  const notify = () => listeners.forEach((fn) => fn())

  const mine = (id) => {
    const record = records.get(id)
    return record && record.listener === currentListener() ? record : undefined
  }

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

  const get = (id) => {
    const record = mine(id)
    if (!record) return undefined
    const ids = trackIdsOf(record)
    const done = ids.filter((tid) => stored.has(tid)).length
    const pending = ids.some((tid) => !stored.has(tid) && !failed.has(tid))
    let state = 'interrupted'
    if (done === ids.length) state = 'downloaded'
    else if (running && pending) state = 'downloading'
    return { id, name: record.playlist.name, state, done, total: ids.length }
  }

  const remove = async (playlistId) => {
    const record = mine(playlistId)
    if (!record) return
    records.delete(playlistId)
    await storage.deleteDownload(playlistId)
    await deleteOrphans(trackIdsOf(record))
  }

  // A Smart Playlist's Tracks change when the server re-evaluates its rules,
  // which doesn't touch its updatedAt
  const hasChanged = (before, now) =>
    Boolean(now.rules) || now.updatedAt !== before.updatedAt

  const refreshOne = async (id) => {
    const before = records.get(id)
    if (!before) return
    const info = await server.getPlaylistInfo(id)
    // Each await below may race a remove or start of the same Download
    const replaced = () => records.get(id) !== before
    if (replaced()) return
    if (!info) return remove(id) // deleted on the server
    if (!hasChanged(before.playlist, info)) return
    const { playlist, tracks } = await server.getPlaylist(id)
    if (replaced()) return
    const record = { listener: before.listener, playlist, tracks }
    records.set(id, record)
    await storage.saveDownload(record)
    if (!records.has(id)) await storage.deleteDownload(id) // removed meanwhile
    await deleteOrphans(trackIdsOf(before))
  }

  const refreshAll = async () => {
    for (const id of [...records.keys()].filter((id) => mine(id))) {
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
      list.forEach((r) => records.set(r.playlist.id, r))
      trackIds.forEach((id) => stored.add(id))
      notify()
    },

    start: async (playlistId) => {
      Promise.resolve(requestPersistence()).catch(() => {})
      const { playlist, tracks } = await server.getPlaylist(playlistId)
      const record = { listener: currentListener(), playlist, tracks }
      await storage.saveDownload(record)
      records.set(playlistId, record)
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
    list: () =>
      [...records.keys()].filter((id) => mine(id)).map((id) => get(id)),
    getRecord: (playlistId) => mine(playlistId),
    isTrackDownloaded: (trackId) => stored.has(trackId),
    subscribe: (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
  }
}
