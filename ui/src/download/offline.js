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
