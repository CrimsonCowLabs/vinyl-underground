import { describe, it, expect, vi, afterEach } from 'vitest'
import { browserStorage, checkAudioResponse } from './browserStorage'

const response = (status, contentType) =>
  // A string body would default the Content-Type to text/plain
  new Response(null, {
    status,
    headers: contentType ? { 'Content-Type': contentType } : {},
  })

describe('checkAudioResponse', () => {
  it('accepts an audio response', () => {
    expect(() =>
      checkAudioResponse(response(200, 'audio/ogg'), 't1'),
    ).not.toThrow()
  })

  it('rejects a failed HTTP response', () => {
    expect(() => checkAudioResponse(response(429, 'audio/ogg'), 't1')).toThrow(
      /HTTP 429/,
    )
  })

  // Subsonic errors (Track not found, transcoding failed) come back as
  // HTTP 200 with a JSON or XML body, which must not be stored as audio
  it.each(['application/json', 'text/xml; charset=utf-8'])(
    'rejects a 200 response of type %s',
    (type) => {
      expect(() => checkAudioResponse(response(200, type), 't1')).toThrow(
        /not audio/,
      )
    },
  )

  it('accepts a response with no Content-Type', () => {
    expect(() => checkAudioResponse(response(200), 't1')).not.toThrow()
  })
})

describe('listTrackSizes', () => {
  // A Cache Storage holding two Tracks stored before sizes were recorded,
  // which can only be measured by reading them
  const fakeCaches = () => {
    const entries = new Map([
      ['/__downloads/track/t1', 'abc'],
      ['/__downloads/track/t2', 'abcdef'],
    ])
    const cache = {
      keys: async () =>
        [...entries.keys()].map((p) => ({ url: `http://x${p}` })),
      match: vi.fn(async (p) =>
        entries.has(p) ? new Response(entries.get(p)) : undefined,
      ),
      delete: async (p) => entries.delete(p),
    }
    return { open: async () => cache, cache }
  }

  afterEach(() => vi.unstubAllGlobals())

  // The meter measures again every time a Track is stored
  it('measures each Track once, however often it is asked', async () => {
    const { open, cache } = fakeCaches()
    vi.stubGlobal('caches', { open })

    expect(await browserStorage.listTrackSizes()).toEqual({ t1: 3, t2: 6 })
    expect(await browserStorage.listTrackSizes()).toEqual({ t1: 3, t2: 6 })
    expect(cache.match).toHaveBeenCalledTimes(2)

    await browserStorage.deleteTrack('t1')
    expect(await browserStorage.listTrackSizes()).toEqual({ t2: 6 })
  })
})
