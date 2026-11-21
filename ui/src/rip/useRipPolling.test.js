import { describe, it, expect } from 'vitest'
import { hasActiveRips } from './useRipPolling'

describe('hasActiveRips', () => {
  it('is true while a Rip is queued or downloading', () => {
    expect(
      hasActiveRips({ a: { status: 'ready' }, b: { status: 'queued' } }),
    ).toBe(true)
    expect(hasActiveRips({ a: { status: 'downloading' } })).toBe(true)
  })

  it('is false once every Rip is ready or failed', () => {
    expect(
      hasActiveRips({ a: { status: 'ready' }, b: { status: 'failed' } }),
    ).toBe(false)
    expect(hasActiveRips({})).toBe(false)
    expect(hasActiveRips(undefined)).toBe(false)
  })
})
