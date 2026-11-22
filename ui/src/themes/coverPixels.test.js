import { describe, it, expect, vi, afterEach } from 'vitest'
import { readCoverPixels } from './coverPixels'

const respond = (status, headers = {}) =>
  vi.fn(() =>
    Promise.resolve(
      new Response(new Blob(['image']), {
        status,
        headers: new Headers(headers),
      }),
    ),
  )

describe('readCoverPixels', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reads the server’s placeholder for a missing cover as no cover', async () => {
    // The server sends its placeholder image with no-store, never a real cover
    vi.stubGlobal('fetch', respond(200, { 'Cache-Control': 'no-store' }))
    await expect(
      readCoverPixels('/rest/getCoverArt?id=al-1'),
    ).resolves.toBeNull()
  })

  it('fails for a cover that fails to load', async () => {
    vi.stubGlobal('fetch', respond(404))
    await expect(readCoverPixels('/rest/getCoverArt?id=al-1')).rejects.toThrow()
  })

  it('fails when offline with no stored cover', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('offline'))),
    )
    await expect(readCoverPixels('/rest/getCoverArt?id=al-1')).rejects.toThrow()
  })
})
