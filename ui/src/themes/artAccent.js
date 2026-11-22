// "From album art": an accent taken from the cover, as the UI refresh
// prototype (#88, prototypes/ui-refresh/app.js) settled it. docs/design.md
// ("UI refresh", "Palette") describes it.

import { contrastRatio } from './contrast'
import { ON_ACCENT, surfaces } from './vinylUndergroundPalette'

const HUE_BINS = 12
// The heaviest hue bin must weigh this much per pixel, or the cover counts as
// grey: 2 in the prototype's 32×32 sample
const MIN_WEIGHT_PER_PIXEL = 2 / (32 * 32)

export const toHex = (rgb) =>
  '#' +
  rgb
    .map((v) => Math.round(v).toString(16).padStart(2, '0'))
    .join('')
    .toUpperCase()

export const fromHex = (hex) =>
  [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))

const hslToRgb = ([h, s, l]) => {
  if (!s) return [l * 255, l * 255, l * 255]
  const f = (p, q, t) => {
    if (t < 0) t += 1
    if (t > 1) t -= 1
    if (t < 1 / 6) return p + (q - p) * 6 * t
    if (t < 1 / 2) return q
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6
    return p
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s
  const p = 2 * l - q
  return [f(p, q, h + 1 / 3), f(p, q, h), f(p, q, h - 1 / 3)].map(
    (v) => v * 255,
  )
}

const rgbToHsl = ([r, g, b]) => {
  r /= 255
  g /= 255
  b /= 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  if (max === min) return [0, 0, l]
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0)
  else if (max === g) h = (b - r) / d + 2
  else h = (r - g) / d + 4
  return [h / 6, s, l]
}

// coverColour is the most vivid colour covering a fair part of a cover, from
// its RGBA pixels (a canvas's getImageData), or null for a cover with no real
// colour. Pixels are put in hue bins, each weighted by its saturation squared
// and by how close its lightness is to the middle, and the heaviest bin's
// weighted average wins.
export const coverColour = (data) => {
  const bins = Array.from({ length: HUE_BINS }, () => ({
    w: 0,
    r: 0,
    g: 0,
    b: 0,
  }))
  const count = data.length / 4
  for (let i = 0; i < data.length; i += 4) {
    const rgb = [data[i], data[i + 1], data[i + 2]]
    const [h, s, l] = rgbToHsl(rgb)
    const w = s * s * Math.max(0, 1 - Math.abs(l - 0.5) * 1.8)
    if (w < 0.05) continue
    const bin = bins[Math.floor(h * HUE_BINS) % HUE_BINS]
    bin.w += w
    bin.r += rgb[0] * w
    bin.g += rgb[1] * w
    bin.b += rgb[2] * w
  }
  const best = bins.reduce((m, b) => (b.w > m.w ? b : m))
  if (!count || best.w / count <= MIN_WEIGHT_PER_PIXEL) return null
  return toHex([best.r / best.w, best.g / best.w, best.b / best.w])
}

const MIN_SATURATION = 0.45
const AA = 4.5

// readableAccent turns a cover colour into an accent for the variant ('dark'
// or 'light'): it keeps the hue, raises the saturation to at least 45%, and
// steps the lightness (up on dark, down on light) until it reaches AA (4.5:1)
// against the background and Surface 2.
export const readableAccent = (hex, mode) => {
  const ground = surfaces[mode]
  const reads = (c) =>
    contrastRatio(c, ground.background) >= AA &&
    contrastRatio(c, ground.surface2) >= AA
  const [h, s, l0] = rgbToHsl(fromHex(hex))
  const sat = Math.max(s, MIN_SATURATION)
  const step = mode === 'dark' ? 0.01 : -0.01
  let l = l0
  let main = toHex(hslToRgb([h, sat, l]))
  for (let i = 0; i < 100 && !reads(main); i++) {
    l = Math.min(1, Math.max(0, l + step))
    main = toHex(hslToRgb([h, sat, l]))
  }
  return { main, contrastText: ON_ACCENT[mode] }
}

// mixHex is the colour `t` (0 to 1) of the way from one #RRGGBB colour to
// another
export const mixHex = (from, to, t) => {
  const a = fromHex(from)
  const b = fromHex(to)
  return toHex(a.map((v, i) => v + (b[i] - v) * t))
}
