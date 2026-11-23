import { useLayoutEffect, useState } from 'react'

// useAtLeastWide is whether the element given the returned ref is at least
// minWidth px wide. It follows the element, not the window, so it changes as
// soon as anything else takes or gives back room beside it: the Now Playing
// panel opening or closing, or the sidebar folding. Until it can measure, or
// where there's no ResizeObserver, it says wide.
export const useAtLeastWide = (minWidth) => {
  const [el, setEl] = useState(null)
  const [wide, setWide] = useState(true)

  // Measured before paint, so the page never shows a frame with the wrong
  // columns
  useLayoutEffect(() => {
    if (!el || typeof ResizeObserver === 'undefined') return
    const check = () => setWide(el.getBoundingClientRect().width >= minWidth)
    const observer = new ResizeObserver(check)
    observer.observe(el)
    check()
    return () => observer.disconnect()
  }, [el, minWidth])

  // setEl is the ref, so the effect runs again once the element mounts
  return [setEl, wide]
}
