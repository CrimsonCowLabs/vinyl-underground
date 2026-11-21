// Service worker routes that let downloaded Tracks play with no network.
// Lives outside sw.js so it can be unit tested: sw.js only loads inside a
// service worker. `match(key)` looks a key up in the Downloads cache and
// `fetch` goes to the network.
import { audioTrackId, coverArtId, coverArtKey, trackAudioKey } from './keys'

// Parses a single `bytes=` range against a body of `size` bytes. Returns
// { start, end } (inclusive), null if there is no usable range, or
// 'unsatisfiable'.
const parseRange = (header, size) => {
  const match = /^bytes=(\d*)-(\d*)$/.exec((header || '').trim())
  if (!match || (match[1] === '' && match[2] === '')) return null
  let start, end
  if (match[1] === '') {
    // Suffix range: the last N bytes
    start = Math.max(size - Number(match[2]), 0)
    end = size - 1
  } else {
    start = Number(match[1])
    end = match[2] === '' ? size - 1 : Math.min(Number(match[2]), size - 1)
  }
  if (start >= size || start > end) return 'unsatisfiable'
  return { start, end }
}

// The media element asks for byte ranges to seek, so a stored Track must
// answer them like the server would.
const rangeResponse = async (request, stored) => {
  const blob = await stored.blob()
  const type = stored.headers.get('Content-Type') || blob.type || 'audio/ogg'
  const size = blob.size
  const range = parseRange(request.headers.get('Range'), size)

  if (range === 'unsatisfiable') {
    return new Response(null, {
      status: 416,
      headers: { 'Content-Range': `bytes */${size}` },
    })
  }
  if (!range) {
    return new Response(blob, {
      status: 200,
      headers: {
        'Content-Type': type,
        'Content-Length': String(size),
        'Accept-Ranges': 'bytes',
      },
    })
  }
  const { start, end } = range
  return new Response(blob.slice(start, end + 1, type), {
    status: 206,
    headers: {
      'Content-Type': type,
      'Content-Length': String(end - start + 1),
      'Content-Range': `bytes ${start}-${end}/${size}`,
      'Accept-Ranges': 'bytes',
    },
  })
}

export const createDownloadRoutes = ({ match, fetch }) => ({
  isAudio: ({ url }) => !!audioTrackId(url),

  // A downloaded Track always plays from the device, online or not
  serveAudio: async ({ url, request }) => {
    const stored = await match(trackAudioKey(audioTrackId(url)))
    return stored ? rangeResponse(request, stored) : fetch(request)
  },

  isCoverArt: ({ url }) => !!coverArtId(url),

  // Cover art comes from the network when it can, so it stays current
  serveCoverArt: async ({ url, request }) => {
    try {
      return await fetch(request)
    } catch (error) {
      const stored = await match(coverArtKey(coverArtId(url)))
      if (stored) return stored
      throw error
    }
  },
})
