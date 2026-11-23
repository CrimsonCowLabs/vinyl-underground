// runVisual plays a visual on a canvas: it owns the frame loop, so every
// visual saves battery the same way.
//
// A visual is { create(canvas, { analyser, theme, ...options }) }, which
// returns a
// renderer, or a Promise of one for a visual that loads its code when chosen
// (Milkdrop, #97). A renderer is:
//
//   draw(now)                 paint one frame; now is the frame's time in ms
//   resize?(width, height, pixelRatio)
//                             the canvas's new size in device pixels, and
//                             how many there are per CSS pixel
//   setTheme?(theme)          the player's theme changed (a new cover's accent)
//   destroy?()                let go of anything it holds
//
// It draws only while the page is visible, and, once playback pauses, for
// SETTLE_MS more so bars sink back, then stops until playback starts again.
// Whoever shows the canvas stops it when the canvas is hidden (another tab, or
// Now Playing closed) by calling stop(), usually by unmounting.

// How long a paused visual keeps drawing, so it comes to rest
export const SETTLE_MS = 1200

// Canvas pixels per CSS pixel, at most: sharper than this costs a phone more
// than it shows
const MAX_PIXEL_RATIO = 2

export const runVisual = (
  canvas,
  visual,
  { analyser, theme, fps = 60, playing = true, ...options } = {},
) => {
  const interval = 1000 / fps
  let renderer = null
  let stopped = false
  let frame = null
  let last = -Infinity
  let settleFrom = null
  let currentTheme = theme

  const fitCanvas = () => {
    const ratio = Math.min(window.devicePixelRatio || 1, MAX_PIXEL_RATIO)
    const width = Math.max(1, Math.round(canvas.clientWidth * ratio))
    const height = Math.max(1, Math.round(canvas.clientHeight * ratio))
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width
      canvas.height = height
    }
    renderer?.resize?.(width, height, ratio)
    drawIfSettled()
  }

  // A canvas that has stopped drawing (paused, settled) is drawn once when
  // it changes, so a resize (which clears it) or a new cover's colours show
  const drawIfSettled = () => {
    if (renderer && !stopped && frame === null && !document.hidden) {
      renderer.draw(performance.now())
    }
  }

  const tick = (now) => {
    frame = null
    if (!renderer || stopped) return
    // A frame early by less than a couple of ms still counts: screens differ
    if (now - last >= interval - 2) {
      last = now
      renderer.draw(now)
    }
    if (!playing) {
      settleFrom ??= now
      if (now - settleFrom > SETTLE_MS) return
    }
    schedule()
  }

  const schedule = () => {
    if (frame === null && renderer && !stopped && !document.hidden) {
      frame = window.requestAnimationFrame(tick)
    }
  }

  const unschedule = () => {
    if (frame !== null) window.cancelAnimationFrame(frame)
    frame = null
  }

  const onVisibility = () => {
    if (document.hidden) {
      unschedule()
    } else {
      settleFrom = null
      schedule()
    }
  }
  document.addEventListener('visibilitychange', onVisibility)

  const resizeObserver =
    typeof ResizeObserver === 'function' ? new ResizeObserver(fitCanvas) : null
  resizeObserver?.observe(canvas)

  const begin = (created) => {
    if (stopped) {
      created?.destroy?.()
      return
    }
    renderer = created
    schedule()
    fitCanvas()
  }

  const created = visual.create(canvas, { analyser, theme, ...options })
  if (typeof created?.then === 'function') {
    // A visual that fails to load leaves the canvas blank; the visual
    // itself says why (Milkdrop without WebGL falls back, #97)
    created.then(begin).catch(() => {})
  } else {
    begin(created)
  }

  return {
    setPlaying(value) {
      playing = value
      settleFrom = null
      schedule()
    },
    setTheme(value) {
      if (value === currentTheme) return
      currentTheme = value
      renderer?.setTheme?.(value)
      drawIfSettled()
    },
    stop() {
      if (stopped) return
      stopped = true
      unschedule()
      document.removeEventListener('visibilitychange', onVisibility)
      resizeObserver?.disconnect()
      renderer?.destroy?.()
      renderer = null
    },
  }
}
