import React from 'react'
import { render } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import JobOrigin from './JobOrigin'
import RipJobPanel from './review/RipJobPanel'
import { stubResizeObserver } from '../common/stubResizeObserver'

// The panel cuts the job's origin short with Clipped, which watches its size.
let restoreResizeObserver
beforeEach(() => {
  restoreResizeObserver = stubResizeObserver().restore
})
afterEach(() => restoreResizeObserver())

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return { ...actual, useTranslate: () => (key) => key }
})

// The card itself is tested on its own; here it only needs to be there.
vi.mock('./review/ReviewCard', () => ({
  default: () => <div data-testid="card" />,
}))

describe('JobOrigin', () => {
  it('labels an upload and a drop, which have no URL (#35)', () => {
    expect(
      render(<JobOrigin record={{ kind: 'upload', url: '' }} />).container
        .textContent,
    ).toBe('resources.rip.kind.upload')
    expect(
      render(<JobOrigin record={{ kind: 'drop', url: '' }} />).container
        .textContent,
    ).toBe('resources.rip.kind.drop')
  })

  it("links to a Rip's URL", () => {
    const url = 'https://www.youtube.com/watch?v=abc'
    const { getByRole } = render(<JobOrigin record={{ kind: 'rip', url }} />)
    expect(getByRole('link').getAttribute('href')).toBe(url)
  })

  it('takes a job from before kinds as a Rip', () => {
    const url = 'https://www.youtube.com/watch?v=abc'
    const { getByRole } = render(<JobOrigin record={{ url }} />)
    expect(getByRole('link').textContent).toBe(url)
  })
})

describe('RipJobPanel', () => {
  it('labels a ready card with where its Files came from', () => {
    const { getByTestId } = render(
      <RipJobPanel
        record={{ id: 'j', status: 'ready', kind: 'drop', items: [] }}
      />,
    )
    expect(getByTestId('job-origin').textContent).toBe(
      'resources.rip.kind.drop',
    )
    expect(getByTestId('card')).toBeInTheDocument()
  })
})
