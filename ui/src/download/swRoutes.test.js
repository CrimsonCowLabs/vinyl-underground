import { describe, it, expect, vi } from 'vitest'
import { createDownloadRoutes } from './swRoutes'
import { trackAudioKey, coverArtKey } from './keys'

const AUDIO = '0123456789'

const setup = ({ cached = {}, network } = {}) => {
  const cache = new Map(Object.entries(cached))
  const match = vi.fn(async (key) => cache.get(key)?.clone())
  const fetch = vi.fn(
    network || (async () => new Response('from network', { status: 200 })),
  )
  return { routes: createDownloadRoutes({ match, fetch }), fetch, match }
}

const audio = () =>
  new Response(AUDIO, { headers: { 'Content-Type': 'audio/ogg' } })

const get = (path, headers = {}) => {
  const url = new URL(path, 'https://music.example')
  return { url, request: new Request(url, { headers }) }
}

// The auth params in a stream URL change on every request, so they must not
// matter when looking up a downloaded Track.
const STREAM =
  '/rest/stream?u=ana&t=tok&s=salt&f=json&v=1.8.0&c=NavidromeUI&id=t1&_=123'
const TRANSCODE =
  '/rest/getTranscodeStream?u=ana&t=x&s=y&mediaId=t1&mediaType=song&transcodeParams=jwt'

describe('createDownloadRoutes', () => {
  describe('audio', () => {
    it.each([STREAM, TRANSCODE, '/music/rest/stream.view?id=t1'])(
      'recognises %s as a Track stream',
      (path) => {
        const { routes } = setup()
        expect(routes.isAudio(get(path))).toBe(true)
      },
    )

    it.each(['/rest/getCoverArt?id=t1', '/api/playlist/pl1', '/rest/stream'])(
      'leaves %s alone',
      (path) => {
        const { routes } = setup()
        expect(routes.isAudio(get(path))).toBe(false)
      },
    )

    it('plays a downloaded Track from the device, whatever its auth params', async () => {
      const { routes, fetch } = setup({
        cached: { [trackAudioKey('t1')]: audio() },
      })

      for (const path of [STREAM, TRANSCODE]) {
        const res = await routes.serveAudio(get(path))
        expect(res.status).toBe(200)
        expect(res.headers.get('Content-Type')).toBe('audio/ogg')
        expect(res.headers.get('Accept-Ranges')).toBe('bytes')
        await expect(res.text()).resolves.toBe(AUDIO)
      }
      expect(fetch).not.toHaveBeenCalled()
    })

    it('streams a Track that is not downloaded from the server', async () => {
      const { routes, fetch } = setup()
      const req = get(STREAM)

      const res = await routes.serveAudio(req)

      await expect(res.text()).resolves.toBe('from network')
      expect(fetch).toHaveBeenCalledWith(req.request)
    })

    it.each([
      ['bytes=2-5', '2345', 'bytes 2-5/10'],
      ['bytes=7-', '789', 'bytes 7-9/10'],
      ['bytes=-3', '789', 'bytes 7-9/10'],
      ['bytes=8-100', '89', 'bytes 8-9/10'],
    ])(
      'answers a Range of %s so the player can seek',
      async (range, body, contentRange) => {
        const { routes } = setup({ cached: { [trackAudioKey('t1')]: audio() } })

        const res = await routes.serveAudio(get(STREAM, { Range: range }))

        expect(res.status).toBe(206)
        expect(res.headers.get('Content-Range')).toBe(contentRange)
        expect(res.headers.get('Content-Length')).toBe(String(body.length))
        await expect(res.text()).resolves.toBe(body)
      },
    )

    it('refuses a Range past the end of the Track', async () => {
      const { routes } = setup({ cached: { [trackAudioKey('t1')]: audio() } })

      const res = await routes.serveAudio(get(STREAM, { Range: 'bytes=10-' }))

      expect(res.status).toBe(416)
      expect(res.headers.get('Content-Range')).toBe('bytes */10')
    })
  })

  describe('cover art', () => {
    const COVER = '/rest/getCoverArt?u=ana&t=x&s=y&id=mf-t1&_=2024&size=600'

    it('uses the network when it can', async () => {
      const { routes, match } = setup()

      const res = await routes.serveCoverArt(get(COVER))

      await expect(res.text()).resolves.toBe('from network')
      expect(match).not.toHaveBeenCalled()
    })

    it.each([COVER, COVER.replace('mf-t1', 'mf-t1_0a1b2c3d4e5f6a7b')])(
      'falls back to the downloaded cover, whatever its size or version, when offline (%s)',
      async (path) => {
        const { routes } = setup({
          cached: { [coverArtKey('mf-t1')]: new Response('stored cover') },
          network: async () => {
            throw new TypeError('Failed to fetch')
          },
        })

        const res = await routes.serveCoverArt(get(path))

        await expect(res.text()).resolves.toBe('stored cover')
      },
    )

    it('rethrows when offline and the cover was never downloaded', async () => {
      const { routes } = setup({
        network: async () => {
          throw new TypeError('Failed to fetch')
        },
      })

      await expect(routes.serveCoverArt(get(COVER))).rejects.toThrow(
        'Failed to fetch',
      )
    })
  })
})
