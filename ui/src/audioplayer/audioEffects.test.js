import { describe, it, expect } from 'vitest'
import { isIOS, selectAudioEffectsAvailable } from './audioEffects'

const IPHONE =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1'
const IPAD_LEGACY =
  'Mozilla/5.0 (iPad; CPU OS 12_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1'
const MAC_SAFARI =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15'
const ANDROID =
  'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36'
const WINDOWS_CHROME =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'

describe('isIOS', () => {
  it('detects an iPhone', () => {
    expect(isIOS({ userAgent: IPHONE, maxTouchPoints: 5 })).toBe(true)
  })

  it('detects an iPad that names itself', () => {
    expect(isIOS({ userAgent: IPAD_LEGACY, maxTouchPoints: 5 })).toBe(true)
  })

  it('detects iPadOS presenting itself as a Mac with a touch screen', () => {
    expect(isIOS({ userAgent: MAC_SAFARI, maxTouchPoints: 5 })).toBe(true)
  })

  it('does not count a desktop Mac', () => {
    expect(isIOS({ userAgent: MAC_SAFARI, maxTouchPoints: 0 })).toBe(false)
  })

  it('does not count Android', () => {
    expect(isIOS({ userAgent: ANDROID, maxTouchPoints: 5 })).toBe(false)
  })

  it('does not count a Windows touch screen', () => {
    expect(isIOS({ userAgent: WINDOWS_CHROME, maxTouchPoints: 10 })).toBe(false)
  })
})

describe('selectAudioEffectsAvailable', () => {
  const state = (audioEffects) => ({ settings: { audioEffects } })

  it('makes effects available off iOS, whatever the setting', () => {
    expect(selectAudioEffectsAvailable(state(false), false)).toBe(true)
    expect(selectAudioEffectsAvailable(state(undefined), false)).toBe(true)
  })

  it('keeps effects unavailable on iOS until the switch is on', () => {
    expect(selectAudioEffectsAvailable(state(false), true)).toBe(false)
    expect(selectAudioEffectsAvailable(state(undefined), true)).toBe(false)
    expect(selectAudioEffectsAvailable({}, true)).toBe(false)
  })

  it('makes effects available on iOS once the switch is on', () => {
    expect(selectAudioEffectsAvailable(state(true), true)).toBe(true)
  })
})
