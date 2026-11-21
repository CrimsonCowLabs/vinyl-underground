// What the app can still show with no network. Downloads answer Playlist and
// album pages (see offlineDataProvider.js), and Personal settings hold the
// storage meter. Every other page needs the server, so offline it is shown as
// unavailable rather than left to fail. A new feature that needs the server
// (e.g. Stations, Generated Playlists) is unavailable offline by default.
const AVAILABLE_OFFLINE = ['', 'album', 'playlist', 'personal']

// The resource or page a route belongs to: its first path segment
const sectionOf = (pathname) => (pathname || '').split('/')[1] || ''

export const isAvailableOffline = (pathname) =>
  AVAILABLE_OFFLINE.includes(sectionOf(pathname))

// Whether the server answers. navigator.onLine only notices a lost
// connection, but on a tailnet the server is usually unreachable with the
// network still up (VPN off, host asleep), which only a failed request
// reveals. While unreachable, probe() is retried until the server answers.
export const createReachability = ({ probe, retryMs = 15000 }) => {
  let reachable = true
  let timer = null
  const subscribers = new Set()

  const set = (value) => {
    if (value === reachable) return
    reachable = value
    subscribers.forEach((fn) => fn())
  }

  const stopChecking = () => {
    clearTimeout(timer)
    timer = null
  }

  const check = () => {
    timer = setTimeout(async () => {
      const answered = await Promise.resolve(probe()).catch(() => false)
      if (reachable) return // a request got through meanwhile
      if (answered) set(true)
      else check()
    }, retryMs)
  }

  return {
    isReachable: () => reachable,
    markReachable: () => {
      stopChecking()
      set(true)
    },
    markUnreachable: () => {
      if (!timer) check()
      set(false)
    },
    subscribe: (fn) => {
      subscribers.add(fn)
      return () => subscribers.delete(fn)
    },
  }
}
