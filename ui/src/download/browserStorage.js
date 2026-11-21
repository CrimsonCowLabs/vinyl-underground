// Stores Downloads in Cache Storage, where the service worker can serve them.
// A thin adapter: the decisions live in downloads.js.
import subsonic from '../subsonic'
import { baseUrl } from '../utils'
import {
  DOWNLOADS_CACHE,
  DOWNLOAD_RECORD_PREFIXES,
  TRACK_AUDIO_PREFIX,
  coverArtKey,
  downloadRecordKey,
  trackAudioKey,
} from './keys'
import { itemOf, kindOf, trackIdOf } from './downloads'

// ADR-0006: Downloads are Opus at ~160 kbps
export const DOWNLOAD_FORMAT = 'opus'
export const DOWNLOAD_BIT_RATE = 160
const COVER_SIZE = 300

const open = () => caches.open(DOWNLOADS_CACHE)

// Subsonic reports errors (Track not found, transcoding failed) as HTTP 200
// with a JSON or XML body, so a 2xx alone doesn't mean audio came back
export const checkAudioResponse = (res, trackId) => {
  if (!res.ok) throw new Error(`Track ${trackId}: HTTP ${res.status}`)
  const type = res.headers.get('Content-Type')
  if (type && !type.startsWith('audio/')) {
    throw new Error(`Track ${trackId}: response is ${type}, not audio`)
  }
}

const keysWithPrefix = async (prefix) => {
  const cache = await open()
  const requests = await cache.keys()
  return requests
    .map((r) => new URL(r.url).pathname)
    .filter((path) => path.startsWith(prefix))
}

const idFromKey = (prefix) => (path) =>
  decodeURIComponent(path.slice(prefix.length))

// Cover art is a nice-to-have (e.g. on the lock screen), so a failure to
// store it never fails the Download
const storeCover = async (cache, coverId, url, signal) => {
  try {
    const res = await fetch(url, { signal })
    if (res.ok) await cache.put(coverArtKey(coverId), res)
  } catch {
    // ignore
  }
}

// The same cover the player and Playlist view ask for (see getCoverArtUrl)
const trackCover = (track) => {
  const trackId = trackIdOf(track)
  return {
    coverId: `mf-${trackId}`,
    url: subsonic.getCoverArtUrl(
      { id: trackId, updatedAt: track.updatedAt, album: track.album },
      COVER_SIZE,
    ),
  }
}

// The same cover the Playlist and album pages ask for (see getCoverArtUrl)
const COVER_PREFIXES = { playlist: 'pl', album: 'al' }
const downloadCoverId = (id, kind) => `${COVER_PREFIXES[kind]}-${id}`

// Each stored Track remembers its size, so the storage meter doesn't have to
// read every Track back to measure it
const SIZE_HEADER = 'X-Download-Size'

// A stored Track's size never changes, and the storage meter asks for every
// Track's size each time one is stored, so each is measured only once
const knownSizes = new Map() // trackId -> bytes

const storedSize = async (res) => {
  const size = Number(res.headers.get(SIZE_HEADER))
  // Tracks stored before sizes were recorded are measured the slow way
  return size > 0 ? size : (await res.blob()).size
}

export const browserStorage = {
  listDownloads: async () => {
    const cache = await open()
    const paths = (
      await Promise.all(DOWNLOAD_RECORD_PREFIXES.map(keysWithPrefix))
    ).flat()
    const records = await Promise.all(
      paths.map((path) => cache.match(path).then((res) => res?.json())),
    )
    return records.filter(Boolean)
  },

  saveDownload: async (record) => {
    const cache = await open()
    const item = itemOf(record)
    const kind = kindOf(record)
    await cache.put(
      downloadRecordKey(item.id, kind),
      new Response(JSON.stringify(record), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await storeCover(
      cache,
      downloadCoverId(item.id, kind),
      subsonic.getCoverArtUrl(item, COVER_SIZE),
    )
  },

  deleteDownload: async (id, kind = 'playlist') => {
    const cache = await open()
    await cache.delete(downloadRecordKey(id, kind))
    await cache.delete(coverArtKey(downloadCoverId(id, kind)))
  },

  listTrackIds: async () =>
    (await keysWithPrefix(TRACK_AUDIO_PREFIX)).map(
      idFromKey(TRACK_AUDIO_PREFIX),
    ),

  listTrackSizes: async () => {
    const cache = await open()
    const paths = await keysWithPrefix(TRACK_AUDIO_PREFIX)
    const entries = await Promise.all(
      paths.map(async (path) => {
        const trackId = idFromKey(TRACK_AUDIO_PREFIX)(path)
        if (!knownSizes.has(trackId)) {
          const res = await cache.match(path)
          if (!res) return [trackId, 0]
          knownSizes.set(trackId, await storedSize(res))
        }
        return [trackId, knownSizes.get(trackId)]
      }),
    )
    return Object.fromEntries(entries)
  },

  // The Track's audio is stored last: once it is there, the Track is done
  saveTrack: async (track, { signal } = {}) => {
    const cache = await open()
    const trackId = trackIdOf(track)
    const { coverId, url } = trackCover(track)
    await storeCover(cache, coverId, url, signal)

    const res = await fetch(
      baseUrl(
        subsonic.url('stream', trackId, {
          format: DOWNLOAD_FORMAT,
          maxBitRate: DOWNLOAD_BIT_RATE,
        }),
      ),
      { signal },
    )
    checkAudioResponse(res, trackId)
    // Read in full to learn its size (a transcoded stream has no
    // Content-Length), then stored as a fresh Response, so none of the
    // stream's headers (e.g. a Content-Range or a short-lived cache policy)
    // are stored with it
    const audio = await res.blob()
    await cache.put(
      trackAudioKey(trackId),
      new Response(audio, {
        headers: {
          'Content-Type': res.headers.get('Content-Type') || 'audio/ogg',
          [SIZE_HEADER]: String(audio.size),
        },
      }),
    )
    knownSizes.set(trackId, audio.size)
  },

  deleteTrack: async (trackId) => {
    const cache = await open()
    knownSizes.delete(trackId)
    await cache.delete(trackAudioKey(trackId))
    await cache.delete(coverArtKey(`mf-${trackId}`))
  },
}
