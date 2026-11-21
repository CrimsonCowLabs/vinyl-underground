import { trackIdOf } from './downloads'

// Answers "is this Track stored on this device?" for each song in a list,
// kept up to date. A Track counts while any of the current Listener's
// Downloads (album or Playlist) holds it and it has finished storing; it stops
// counting when the last of them is removed.
//
// The answers come from the Downloads in memory, never from the browser's
// storage, and are worked out once per change in the Downloads however many
// rows are on screen: while any row is subscribed, the stored Track ids are
// kept in a set, and only the rows whose answer changed are told.
export const createStoredTracks = (downloads) => {
  const read = () => new Set(downloads.downloadedTracks().map(trackIdOf))
  const subscribers = new Map() // Track id -> Set of callbacks
  let stored = null // while anyone is subscribed
  let unsubscribe = null

  const update = () => {
    const before = stored
    stored = read()
    const changed = [...subscribers.keys()].filter(
      (tid) => before.has(tid) !== stored.has(tid),
    )
    changed.forEach((tid) => subscribers.get(tid)?.forEach((fn) => fn()))
  }

  // Before anyone has subscribed (e.g. while a long list renders its rows for
  // the first time), one read serves every row asking in the same tick
  let snapshot = null
  const current = () => {
    if (stored) return stored
    if (!snapshot) {
      snapshot = read()
      queueMicrotask(() => (snapshot = null))
    }
    return snapshot
  }

  return {
    has: (trackId) => current().has(trackId),

    subscribe: (trackId, fn) => {
      if (!subscribers.size) {
        stored = read()
        unsubscribe = downloads.subscribe(update)
      }
      if (!subscribers.has(trackId)) subscribers.set(trackId, new Set())
      subscribers.get(trackId).add(fn)
      return () => {
        const fns = subscribers.get(trackId)
        if (!fns?.delete(fn)) return
        if (!fns.size) subscribers.delete(trackId)
        if (!subscribers.size) {
          unsubscribe()
          unsubscribe = null
          stored = null
        }
      }
    },
  }
}
