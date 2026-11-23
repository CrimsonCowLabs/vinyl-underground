// The lightweight visuals (#96), drawn with canvas 2D: no WebGL, and only a
// few shapes a frame, so a phone keeps up. Each is a visual for runVisual:
// { create(canvas, { analyser, theme }) }, returning a renderer.
//
// theme comes from useVisualTheme: { accent, glow, cover, mode }. resize gets
// the canvas's size in device pixels and how many there are per CSS pixel.
import { alpha } from '@material-ui/core/styles'
import { bandLevels, logBands, smooth, wavePoints } from './visualMath'

const clamp = (value, min, max) => Math.min(max, Math.max(min, value))

const sampleRate = (analyser) => analyser.context?.sampleRate || 48000

// Spectrum: bars from the bass on the left to the treble on the right, about
// one every 12 CSS px, rising with a beat and sinking back slowly.
const spectrum = {
  create(canvas, { analyser, theme }) {
    const context = canvas.getContext('2d')
    const data = new Uint8Array(analyser.frequencyBinCount)
    const levels = []
    let bands = []
    let width = canvas.width
    let height = canvas.height
    let ratio = 1
    let colours = theme

    const resize = (w, h, pixelRatio = 1) => {
      width = w
      height = h
      ratio = pixelRatio
      const count = clamp(Math.round(w / ratio / 12), 16, 64)
      bands = logBands(count, {
        sampleRate: sampleRate(analyser),
        binCount: analyser.frequencyBinCount,
      })
    }
    resize(width, height)

    return {
      resize,
      setTheme: (value) => (colours = value),
      draw() {
        analyser.getByteFrequencyData(data)
        smooth(levels, bandLevels(data, bands), { rise: 0.55, fall: 0.1 })
        context.clearRect(0, 0, width, height)
        // A margin each side, so the outer bars don't touch the edges
        const margin = width * 0.05
        const slot = (width - 2 * margin) / levels.length
        const bar = slot * 0.62
        const floor = height * 0.92
        const minimum = 2 * ratio
        const fill = context.createLinearGradient(
          0,
          floor - height * 0.84,
          0,
          floor,
        )
        fill.addColorStop(0, colours.glow)
        fill.addColorStop(1, colours.accent)
        context.fillStyle = fill
        for (let i = 0; i < levels.length; i++) {
          const h = Math.max(minimum, levels[i] ** 1.4 * height * 0.84)
          const x = margin + i * slot + (slot - bar) / 2
          context.fillRect(x, floor - h, bar, h)
        }
      },
    }
  },
}

// Waveform: the sound's wave across the middle, drawn as a line with a wider,
// fainter one under it.
const waveform = {
  create(canvas, { analyser, theme }) {
    const context = canvas.getContext('2d')
    const data = new Uint8Array(analyser.fftSize)
    const points = []
    let width = canvas.width
    let height = canvas.height
    let ratio = 1
    let colours = theme

    const trace = (count) => {
      const middle = height / 2
      const amplitude = height * 0.42
      context.beginPath()
      for (let i = 0; i < count; i++) {
        const x = (i / (count - 1)) * width
        const y = middle - points[i] * amplitude
        if (i === 0) context.moveTo(x, y)
        else context.lineTo(x, y)
      }
    }

    return {
      resize: (w, h, pixelRatio = 1) => {
        width = w
        height = h
        ratio = pixelRatio
      },
      setTheme: (value) => (colours = value),
      draw() {
        analyser.getByteTimeDomainData(data)
        const count = clamp(Math.round(width / ratio / 4), 64, 512)
        smooth(points, wavePoints(data, count), { rise: 0.5, fall: 0.5 })
        context.clearRect(0, 0, width, height)
        context.lineJoin = 'round'
        context.lineCap = 'round'
        trace(count)
        context.strokeStyle = colours.glow
        context.lineWidth = 9 * ratio
        context.stroke()
        context.strokeStyle = colours.accent
        context.lineWidth = 2.5 * ratio
        context.stroke()
      },
    }
  },
}

// How much bigger the glow's canvas is than the cover it surrounds, in Now
// Playing's art area: as much as fits across a phone's view
export const GLOW_SPREAD = 1.3

// Glow: light in the cover's colour spilling out around the album art and
// swelling with the bass, drawn as the blurred shadow of a square under the
// cover. It changes gently (slow to rise, slower to fall), never flashing.
// The cover sits in the middle of the canvas, taking coverShare of its
// shorter side.
const glow = {
  create(canvas, { analyser, theme, coverShare = 1 / GLOW_SPREAD }) {
    const context = canvas.getContext('2d')
    const data = new Uint8Array(analyser.frequencyBinCount)
    const level = [0]
    let bass = []
    const share = coverShare
    let width = canvas.width
    let height = canvas.height
    let colours = theme

    const resize = (w, h) => {
      width = w
      height = h
      bass = logBands(4, {
        sampleRate: sampleRate(analyser),
        binCount: analyser.frequencyBinCount,
        minHz: 40,
        maxHz: 250,
      })
    }
    resize(width, height)

    return {
      resize,
      setTheme: (value) => (colours = value),
      draw() {
        analyser.getByteFrequencyData(data)
        const levels = bandLevels(data, bass)
        const energy = levels.reduce((sum, l) => sum + l, 0) / levels.length
        smooth(level, [energy ** 2], { rise: 0.2, fall: 0.04 })
        const swell = level[0]
        const shorter = Math.min(width, height)
        const side = shorter * share
        // The room around the cover, which the glow fills more of as the
        // bass swells
        const room = (shorter - side) / 2
        const strength = colours.mode === 'light' ? 0.75 : 0.95
        const inset = side * 0.02
        const square = () =>
          context.fillRect(
            (width - side) / 2 + inset,
            (height - side) / 2 + inset,
            side - 2 * inset,
            side - 2 * inset,
          )
        context.clearRect(0, 0, width, height)
        context.fillStyle = colours.cover
        // A wide, soft halo that reaches out with the bass...
        context.shadowColor = alpha(
          colours.cover,
          strength * (0.6 + 0.4 * swell),
        )
        context.shadowBlur = room * (0.9 + 0.7 * swell)
        square()
        // ...over a brighter rim close to the cover
        context.shadowColor = alpha(colours.cover, strength)
        context.shadowBlur = room * 0.35
        square()
      },
    }
  },
}

export const RENDERERS = { spectrum, waveform, glow }
