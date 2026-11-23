import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { Clipped } from './Clipped'
import { stubResizeObserver } from './stubResizeObserver'

describe('Clipped', () => {
  let restore
  // jsdom lays nothing out, so the text is made to look wider than its cell
  beforeEach(() => {
    ;({ restore } = stubResizeObserver())
    Object.defineProperty(HTMLElement.prototype, 'scrollWidth', {
      configurable: true,
      get: () => 500,
    })
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
      configurable: true,
      get: () => 100,
    })
  })
  afterEach(() => {
    restore()
    delete HTMLElement.prototype.scrollWidth
    delete HTMLElement.prototype.clientWidth
  })

  it('shows the full text on hover when it is cut short', async () => {
    const path = 'Inbox/Some Artist/Some Album (Deluxe Edition) [2024]'
    render(<Clipped title={path} />)
    const text = screen.getByText(path)
    expect(text).toHaveStyle({ textOverflow: 'ellipsis', whiteSpace: 'nowrap' })

    fireEvent.mouseOver(text)

    expect(await screen.findByRole('tooltip')).toHaveTextContent(path)
  })

  it('shows nothing for an empty cell', () => {
    const { container } = render(<Clipped title="" />)
    expect(container).toBeEmptyDOMElement()
  })
})
