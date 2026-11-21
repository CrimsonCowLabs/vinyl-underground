import React, { cloneElement } from 'react'
import { render } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import rip from './index'

describe('the Inbox resource', () => {
  // The sidebar's MenuItemLink renders cloneElement(leftIcon, ...)
  it('gives the sidebar an icon element it can clone and render', () => {
    expect(React.isValidElement(rip.icon)).toBe(true)
    const { container } = render(
      <div>{cloneElement(rip.icon, { title: 'Inbox' })}</div>,
    )
    expect(container.querySelector('svg')).not.toBeNull()
  })
})
