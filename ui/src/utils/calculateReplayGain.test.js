import { describe, it, expect } from 'vitest'
import { replayGainRequest } from './calculateReplayGain'

const song = {
  rgTrackGain: -6,
  rgTrackPeak: 0.5,
  rgAlbumGain: -12,
  rgAlbumPeak: 0.5,
}

describe('replayGainRequest', () => {
  it("asks for the Track's gain in track mode", () => {
    const gain = replayGainRequest(true, { gainMode: 'track', preAmp: 0 }, song)
    expect(gain).toBeCloseTo(0.501, 3) // -6 dB
  })

  it("asks for the album's gain in album mode", () => {
    const gain = replayGainRequest(true, { gainMode: 'album', preAmp: 0 }, song)
    expect(gain).toBeCloseTo(0.251, 3) // -12 dB
  })

  it('adds the preamp, limited by the peak so it never clips', () => {
    expect(
      replayGainRequest(true, { gainMode: 'track', preAmp: 6 }, song),
    ).toBeCloseTo(1, 3)
    expect(
      replayGainRequest(true, { gainMode: 'track', preAmp: 15 }, song),
    ).toBe(2) // 1 / peak
  })

  it('asks for unity gain when the Track has no ReplayGain tags', () => {
    expect(replayGainRequest(true, { gainMode: 'track', preAmp: 0 }, {})).toBe(
      1,
    )
  })

  it('asks for nothing when ReplayGain is disabled', () => {
    expect(
      replayGainRequest(true, { gainMode: 'none', preAmp: 0 }, song),
    ).toBeNull()
  })

  it('asks for nothing when the server turned ReplayGain off', () => {
    expect(
      replayGainRequest(false, { gainMode: 'track', preAmp: 0 }, song),
    ).toBeNull()
  })
})
