import React from 'react'
import { act, render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { useAtLeastWide } from './useAtLeastWide'
import { stubResizeObserver } from './stubResizeObserver'

// resize(w) gives the observed element a width and tells the observers, as
// the browser does when the panel opens or closes
let observers
let width
const resize = (w) =>
  act(() => {
    width = w
    observers.forEach((o) => o.callback())
  })

const Probe = () => {
  const [ref, wide] = useAtLeastWide(1000)
  return (
    <div ref={ref} data-testid="probe">
      {wide ? 'wide' : 'narrow'}
    </div>
  )
}

describe('useAtLeastWide', () => {
  let restore, savedRect
  beforeEach(() => {
    savedRect = Element.prototype.getBoundingClientRect
    width = 1200
    ;({ observers, restore } = stubResizeObserver())
    Element.prototype.getBoundingClientRect = function () {
      return { width }
    }
  })
  afterEach(() => {
    restore()
    Element.prototype.getBoundingClientRect = savedRect
  })

  it('follows the width of its element, not the window', () => {
    width = 800
    render(<Probe />)
    expect(screen.getByTestId('probe').textContent).toBe('narrow')

    resize(1000)
    expect(screen.getByTestId('probe').textContent).toBe('wide')

    resize(999)
    expect(screen.getByTestId('probe').textContent).toBe('narrow')
  })

  it('stops watching once unmounted', () => {
    const { unmount } = render(<Probe />)
    expect(observers.size).toBe(1)
    unmount()
    expect(observers.size).toBe(0)
  })

  it('says wide where it cannot measure', () => {
    delete window.ResizeObserver
    render(<Probe />)
    expect(screen.getByTestId('probe').textContent).toBe('wide')
  })
})
