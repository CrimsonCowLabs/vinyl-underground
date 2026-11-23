import { describe, it, expect } from 'vitest'
import { bandLevels, logBands, smooth, wavePoints } from './visualMath'

// An AnalyserNode's defaults: fftSize 2048 at 48 kHz, so 1024 bins of
// 23.4375 Hz each
const ANALYSER = { sampleRate: 48000, binCount: 1024 }

describe('logBands', () => {
  it('spreads bars evenly over octaves, from the bass up', () => {
    const bands = logBands(4, { ...ANALYSER, minHz: 100, maxHz: 1600 })
    // 100–200, 200–400, 400–800, 800–1600 Hz
    expect(bands).toEqual([
      { from: 4, to: 9 },
      { from: 9, to: 17 },
      { from: 17, to: 34 },
      { from: 34, to: 68 },
    ])
  })

  it('gives every bar at least one bin of its own, even in the deep bass', () => {
    const bands = logBands(32, { ...ANALYSER, minHz: 30, maxHz: 16000 })
    expect(bands).toHaveLength(32)
    bands.forEach((band, i) => {
      expect(band.to).toBeGreaterThan(band.from)
      if (i > 0) expect(band.from).toBeGreaterThanOrEqual(bands[i - 1].to)
    })
    expect(bands[31].to).toBeLessThanOrEqual(1024)
  })
})

describe('bandLevels', () => {
  it('is the loudest bin in each bar, from 0 to 1', () => {
    const data = Uint8Array.from([0, 255, 51, 102, 0, 0])
    expect(
      bandLevels(data, [
        { from: 0, to: 2 },
        { from: 2, to: 4 },
        { from: 4, to: 6 },
      ]),
    ).toEqual([1, 0.4, 0])
  })
})

describe('smooth', () => {
  it('rises quickly and falls slowly, so bars do not flicker', () => {
    const levels = [0, 1]
    smooth(levels, [1, 0], { rise: 0.5, fall: 0.1 })
    expect(levels).toEqual([0.5, 0.9])
    smooth(levels, [1, 0], { rise: 0.5, fall: 0.1 })
    expect(levels[0]).toBeCloseTo(0.75)
    expect(levels[1]).toBeCloseTo(0.81)
  })

  it('takes a new length when the bar count changes', () => {
    const levels = [0.5]
    smooth(levels, [1, 1, 1], { rise: 1, fall: 1 })
    expect(levels).toEqual([1, 1, 1])
  })
})

describe('wavePoints', () => {
  it('samples the waveform evenly, centred on silence', () => {
    // 128 is silence in an analyser's byte time-domain data
    expect(
      wavePoints(Uint8Array.from([128, 255, 128, 0, 128, 192, 128, 64]), 4),
    ).toEqual([0, 0, 0, 0])
    expect(
      wavePoints(Uint8Array.from([255, 128, 0, 128, 192, 128, 64, 128]), 4),
    ).toEqual([127 / 128, -1, 64 / 128, -64 / 128])
  })
})
