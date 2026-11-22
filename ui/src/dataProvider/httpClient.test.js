import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fetchImage } from './httpClient'

// A fetch Response, as much of one as fetchImage reads
const fakeResponse = ({
  status = 200,
  type = 'image/png',
  headers = {},
} = {}) => ({
  ok: status >= 200 && status < 300,
  status,
  headers: new Headers({ 'Content-Type': type, ...headers }),
  blob: () => Promise.resolve(new Blob(['img'], { type })),
})

// A token jwtDecode can read: { uid: 'u2' }
const refreshedToken = `x.${btoa(JSON.stringify({ uid: 'u2' }))}.y`

describe('fetchImage', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('token', 'secret-token')
    fetchMock = vi.fn(() => Promise.resolve(fakeResponse()))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the login token in the Authorization header, never in the URL', async () => {
    const blob = await fetchImage('/api/albumCover/al1/candidates/c1/preview')

    expect(blob.type).toBe('image/png')
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/albumCover/al1/candidates/c1/preview')
    expect(url).not.toContain('secret-token')
    expect(options.headers.get('X-ND-Authorization')).toBe(
      'Bearer secret-token',
    )
  })

  it('keeps a refreshed token, as other requests do', async () => {
    fetchMock.mockResolvedValue(
      fakeResponse({ headers: { 'X-ND-Authorization': refreshedToken } }),
    )
    await fetchImage('/api/x')
    expect(localStorage.getItem('token')).toBe(refreshedToken)
    expect(localStorage.getItem('userId')).toBe('u2')
  })

  it('rejects an answer that failed, or isn’t an image', async () => {
    fetchMock.mockResolvedValueOnce(fakeResponse({ status: 502 }))
    await expect(fetchImage('/api/x')).rejects.toMatchObject({ status: 502 })

    fetchMock.mockResolvedValueOnce(fakeResponse({ type: 'text/html' }))
    await expect(fetchImage('/api/x')).rejects.toThrow('not an image')
  })

  it('can be cancelled', async () => {
    const controller = new AbortController()
    await fetchImage('/api/x', { signal: controller.signal })
    expect(fetchMock.mock.calls[0][1].signal).toBe(controller.signal)
  })
})
