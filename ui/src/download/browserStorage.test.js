import { describe, it, expect } from 'vitest'
import { checkAudioResponse } from './browserStorage'

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
