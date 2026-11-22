import { describe, it, expect } from 'vitest'
import { coverColour, readableAccent } from './artAccent'
import { contrastRatio } from './contrast'
import { surfaces } from './vinylUndergroundPalette'

// RGBA pixels, as a canvas's getImageData gives them
const pixels = (...runs) => {
  const out = []
  runs.forEach(([count, [r, g, b]]) => {
    for (let i = 0; i < count; i++) out.push(r, g, b, 255)
  })
  return Uint8ClampedArray.from(out)
}

describe('coverColour', () => {
  it('is the colour of a cover in one vivid colour', () => {
    expect(coverColour(pixels([64, [200, 40, 40]]))).toBe('#C82828')
  })

  it('prefers a vivid colour to a larger muted one', () => {
    // a dusty blue background with a strong orange band
    expect(
      coverColour(pixels([700, [110, 120, 140]], [300, [240, 130, 20]])),
    ).toBe('#F08214')
  })

  it('averages the shades of the heaviest hue', () => {
    expect(
      coverColour(
        pixels([10, [0, 160, 0]], [10, [95, 255, 95]], [5, [200, 0, 0]]),
      ),
    ).toBe('#30D030')
  })

  it('is null for a grey cover with only a speck of colour', () => {
    expect(
      coverColour(pixels([1020, [128, 128, 128]], [4, [200, 40, 40]])),
    ).toBeNull()
  })

  it('is null for a cover with no pixels', () => {
    expect(coverColour(new Uint8ClampedArray(0))).toBeNull()
  })

  it('is null for a grey cover', () => {
    expect(
      coverColour(pixels([300, [128, 128, 128]], [300, [20, 20, 20]])),
    ).toBeNull()
  })
})

// Hue (0-360) and saturation (0-1) of a #RRGGBB colour
const hueAndSaturation = (hex) => {
  const [r, g, b] = [1, 3, 5].map(
    (i) => parseInt(hex.slice(i, i + 2), 16) / 255,
  )
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const d = max - min
  const l = (max + min) / 2
  const s = d === 0 ? 0 : d / (1 - Math.abs(2 * l - 1))
  let h = 0
  if (d && max === r) h = ((g - b) / d + 6) % 6
  else if (d && max === g) h = (b - r) / d + 2
  else if (d) h = (r - g) / d + 4
  return [h * 60, s]
}

describe('readableAccent', () => {
  it('keeps a cover colour that already reads, with dark text on it in the dark variant', () => {
    expect(readableAccent('#3EC1B0', 'dark')).toEqual({
      main: '#3EC1B0',
      contrastText: '#0F1216',
    })
  })

  it('keeps a cover colour that already reads, with white text on it in the light variant', () => {
    expect(readableAccent('#086A5F', 'light')).toEqual({
      main: '#086A5F',
      contrastText: '#FFFFFF',
    })
  })

  const coverColours = [
    '#1A0A40', // deep indigo
    '#5A1010', // oxblood
    '#F4E06A', // pale yellow
    '#C82828', // red
    '#8C9C88', // dusty sage
    '#20D0FF', // cyan
    '#FF2EC8', // magenta
  ]

  describe.each(['dark', 'light'])('%s', (mode) => {
    const s = surfaces[mode]

    it.each(coverColours)(
      'makes %s pass AA (4.5:1) on the background and Surface 2, and under its text',
      (hex) => {
        const { main, contrastText } = readableAccent(hex, mode)
        expect(contrastRatio(main, s.background)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(main, s.surface2)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(contrastText, main)).toBeGreaterThanOrEqual(4.5)
      },
    )

    it.each(coverColours)(
      'keeps the hue of %s and gives it at least 45%% saturation',
      (hex) => {
        const [hue] = hueAndSaturation(hex)
        const [h, sat] = hueAndSaturation(readableAccent(hex, mode).main)
        const apart = Math.abs(h - hue)
        expect(Math.min(apart, 360 - apart)).toBeLessThan(4)
        expect(sat).toBeGreaterThanOrEqual(0.44)
      },
    )
  })
})
