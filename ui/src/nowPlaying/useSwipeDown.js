import { useRef } from 'react'

// How far down a drag has to go to close, in px
export const SWIPE_CLOSE_PX = 120

// Controls a touch on them may be dragging sideways or along, not swiping
const IGNORE = '.MuiSlider-root, .MuiRating-root, [data-no-swipe]'

const point = (e) => {
  const t = e.touches?.[0] || e.changedTouches?.[0]
  return t ? { x: t.clientX, y: t.clientY } : null
}

// useSwipeDown closes a sheet with a downward drag. It returns the touch
// handlers to spread on the sheet's scroller, and a ref for the sheet, which
// follows the finger (moved directly, so a drag doesn't re-render the view).
// A drag that starts on a slider, or while the sheet is scrolled down, is
// left alone.
export const useSwipeDown = (onClose) => {
  const start = useRef(null)
  const sheetRef = useRef(null)
  const setOffset = (px) => {
    const sheet = sheetRef.current
    if (!sheet) return
    sheet.style.transform = px ? `translateY(${px}px)` : ''
    sheet.style.transition = px ? 'none' : ''
  }

  const onTouchStart = (e) => {
    const scroller = e.currentTarget
    const ignored = e.target.closest?.(IGNORE)
    start.current = !ignored && scroller.scrollTop <= 0 ? point(e) : null
  }

  const delta = (e) => {
    const p = point(e)
    if (!start.current || !p) return null
    return { dx: p.x - start.current.x, dy: p.y - start.current.y }
  }

  const onTouchMove = (e) => {
    const d = delta(e)
    if (!d) return
    // Mostly down: follow the finger. Anything else isn't a swipe.
    setOffset(d.dy > 0 && Math.abs(d.dx) < d.dy ? d.dy : 0)
  }

  const onTouchEnd = (e) => {
    const d = delta(e)
    start.current = null
    setOffset(0)
    if (d && d.dy >= SWIPE_CLOSE_PX && Math.abs(d.dx) < d.dy / 2) onClose()
  }

  return {
    sheetRef,
    handlers: {
      onTouchStart,
      onTouchMove,
      onTouchEnd,
      onTouchCancel: () => {
        start.current = null
        setOffset(0)
      },
    },
  }
}
