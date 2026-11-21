import { describe, it, expect } from 'vitest'
import { isAvailableOffline } from './offline'

describe('isAvailableOffline', () => {
  it.each([
    '/',
    '/album',
    '/album/random',
    '/album/al1/show',
    '/playlist',
    '/playlist/pl1/show',
    '/personal',
  ])('opens %s, which Downloads can answer', (path) => {
    expect(isAvailableOffline(path)).toBe(true)
  })

  // Anything else needs the server, including features not built yet
  // (e.g. Stations), so it is unavailable unless listed
  it.each(['/artist', '/song', '/radio', '/station', '/share', '/user/1'])(
    'shows %s as unavailable',
    (path) => {
      expect(isAvailableOffline(path)).toBe(false)
    },
  )
})
