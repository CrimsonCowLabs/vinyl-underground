import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { runVisual, SETTLE_MS } from './runVisual'

// A fake requestAnimationFrame: frames run only when the test says, at the
// time it says
let callbacks
let now
const frame = (ms = 16) => {
  now += ms
  const due = callbacks
  callbacks = new Map()
  due.forEach((callback) => callback(now))
}
const frames = (count, ms) => {
  for (let i = 0; i < count; i++) frame(ms)
}

let hidden
const setHidden = (value) => {
  hidden = value
  document.dispatchEvent(new Event('visibilitychange'))
}

const fakeVisual = () => {
  const renderer = { draw: vi.fn(), setTheme: vi.fn(), destroy: vi.fn() }
  return { renderer, visual: { create: vi.fn(() => renderer) } }
}

const canvas = () => document.createElement('canvas')
const analyser = { frequencyBinCount: 1024 }

describe('runVisual', () => {
  beforeEach(() => {
    callbacks = new Map()
    now = 0
    let id = 0
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      callbacks.set(++id, cb)
      return id
    })
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation((id) =>
      callbacks.delete(id),
    )
    hidden = false
    vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
  })
  afterEach(() => vi.restoreAllMocks())

  it('draws a frame each time the browser paints, until it is stopped', () => {
    const { visual, renderer } = fakeVisual()
    const run = runVisual(canvas(), visual, { analyser, theme: 'dark' })
    expect(visual.create).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), {
      analyser,
      theme: 'dark',
    })

    frames(3)
    expect(renderer.draw).toHaveBeenCalledTimes(3)

    run.stop()
    frames(3)
    expect(renderer.draw).toHaveBeenCalledTimes(3)
    expect(renderer.destroy).toHaveBeenCalled()
    expect(callbacks.size).toBe(0)
  })

  it('stops drawing while the app is in the background', () => {
    const { visual, renderer } = fakeVisual()
    const run = runVisual(canvas(), visual, { analyser })
    frames(2)

    setHidden(true)
    expect(callbacks.size).toBe(0)
    frames(5)
    expect(renderer.draw).toHaveBeenCalledTimes(2)

    setHidden(false)
    frames(2)
    expect(renderer.draw).toHaveBeenCalledTimes(4)

    run.stop()
    setHidden(true)
    setHidden(false)
    expect(callbacks.size).toBe(0)
  })

  it('does not start drawing in the background', () => {
    hidden = true
    const { visual, renderer } = fakeVisual()
    runVisual(canvas(), visual, { analyser })
    frames(3)
    expect(renderer.draw).not.toHaveBeenCalled()
  })

  it('settles for a moment after playback pauses, then stops drawing', () => {
    const { visual, renderer } = fakeVisual()
    const run = runVisual(canvas(), visual, { analyser })
    run.setPlaying(false)
    frames(Math.ceil(SETTLE_MS / 16) + 5)
    const drawn = renderer.draw.mock.calls.length
    expect(drawn).toBeGreaterThan(SETTLE_MS / 20)
    expect(callbacks.size).toBe(0)

    run.setPlaying(true)
    frames(2)
    expect(renderer.draw).toHaveBeenCalledTimes(drawn + 2)
  })

  it('draws no faster than the frame rate asked for', () => {
    const { visual, renderer } = fakeVisual()
    runVisual(canvas(), visual, { analyser, fps: 30 })
    // a 120 Hz screen: only every fourth paint is drawn
    frames(12, 1000 / 120)
    expect(renderer.draw).toHaveBeenCalledTimes(3)
  })

  it('passes a theme change on to the visual', () => {
    const { visual, renderer } = fakeVisual()
    const run = runVisual(canvas(), visual, { analyser, theme: 'dark' })
    run.setTheme('light')
    expect(renderer.setTheme).toHaveBeenCalledWith('light')
  })

  it('waits for a visual that loads, and drops it if stopped first', async () => {
    const { renderer } = fakeVisual()
    let loaded
    const visual = { create: () => new Promise((r) => (loaded = r)) }

    const run = runVisual(canvas(), visual, { analyser })
    frames(2)
    loaded(renderer)
    await Promise.resolve()
    frames(2)
    expect(renderer.draw).toHaveBeenCalledTimes(2)
    run.stop()

    const second = fakeVisual().renderer
    const early = runVisual(canvas(), visual, { analyser })
    early.stop()
    loaded(second)
    await Promise.resolve()
    frames(2)
    expect(second.draw).not.toHaveBeenCalled()
    expect(second.destroy).toHaveBeenCalled()
  })

  it('shows a new theme even once it has settled after a pause', () => {
    const { visual, renderer } = fakeVisual()
    const run = runVisual(canvas(), visual, { analyser, theme: 'dark' })
    run.setPlaying(false)
    frames(Math.ceil(SETTLE_MS / 16) + 5)
    const drawn = renderer.draw.mock.calls.length

    run.setTheme('light')
    expect(renderer.draw).toHaveBeenCalledTimes(drawn + 1)
    expect(callbacks.size).toBe(0)
  })
})
