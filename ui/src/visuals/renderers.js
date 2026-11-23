// The lightweight visuals (#96), drawn with canvas 2D: no WebGL, and only a
// few shapes a frame, so a phone keeps up. Each is a visual for runVisual:
// { create(canvas, { analyser, theme }) }, returning a renderer.
//
// theme is a visualTheme (see visualTheme.js): { accent, glow, cover, mode }.
import { fade } from '@material-ui/core/styles'
import { bandLevels, logBands, smooth, wavePoints } from './visualMath'

const clamp = (value, min, max) => Math.min(max, Math.max(min, value))

const pixelRatio = () =>
  Math.min(typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1, 2)

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
    let colours = theme

    const resize = (w, h) => {
      width = w
      height = h
      const count = clamp(Math.round(w / pixelRatio() / 12), 16, 64)
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
        const slot = width / levels.length
        const bar = slot * 0.62
        const floor = height * 0.92
        const minimum = 2 * pixelRatio()
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
          const x = i * slot + (slot - bar) / 2
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
      resize: (w, h) => {
        width = w
        height = h
      },
      setTheme: (value) => (colours = value),
      draw() {
        analyser.getByteTimeDomainData(data)
        const count = clamp(Math.round(width / pixelRatio() / 4), 64, 512)
        smooth(points, wavePoints(data, count), { rise: 0.5, fall: 0.5 })
        context.clearRect(0, 0, width, height)
        context.lineJoin = 'round'
        context.lineCap = 'round'
        trace(count)
        context.strokeStyle = colours.glow
        context.lineWidth = 9 * pixelRatio()
        context.stroke()
        context.strokeStyle = colours.accent
        context.lineWidth = 2.5 * pixelRatio()
        context.stroke()
      },
    }
  },
}

// How much bigger the glow's canvas is than the cover it surrounds
export const GLOW_SPREAD = 1.5

// Glow: light in the cover's colour around the album art, swelling with the
// bass. It changes gently (slow to rise and slower to fall), never flashing.
const glow = {
  create(canvas, { analyser, theme }) {
    const context = canvas.getContext('2d')
    const data = new Uint8Array(analyser.frequencyBinCount)
    const level = [0]
    let bass = []
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
        smooth(level, [energy ** 2], { rise: 0.25, fall: 0.05 })
        const swell = level[0]
        const x = width / 2
        const y = height / 2
        const cover = Math.min(width, height) / GLOW_SPREAD / 2
        const outer = cover * (1.05 + 0.45 * swell)
        context.clearRect(0, 0, width, height)
        const light = context.createRadialGradient(
          x,
          y,
          cover * 0.6,
          x,
          y,
          outer,
        )
        const strength = colours.mode === 'light' ? 0.55 : 0.7
        light.addColorStop(
          0,
          fade(colours.cover, strength * (0.45 + 0.55 * swell)),
        )
        light.addColorStop(
          0.55,
          fade(colours.cover, strength * 0.35 * (0.3 + swell)),
        )
        light.addColorStop(1, fade(colours.cover, 0))
        context.fillStyle = light
        context.fillRect(0, 0, width, height)
      },
    }
  },
}

export const RENDERERS = { spectrum, waveform, glow }
