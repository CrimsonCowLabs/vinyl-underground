import React from 'react'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import PromoteButton from './PromoteButton.jsx'
import ReviewCard from './ReviewCard.jsx'

const { mockConfig } = vi.hoisted(() => ({
  mockConfig: { enablePromote: true },
}))
vi.mock('../../config', () => ({ default: mockConfig }))

const notify = vi.fn()
const refresh = vi.fn()
const http = vi.fn()

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    useTranslate: () => (key, opts) =>
      opts && Object.keys(opts).some((k) => k !== '_')
        ? `${key} ${JSON.stringify(opts)}`
        : key,
    useNotify: () => notify,
    useRefresh: () => refresh,
  }
})

vi.mock('../../dataProvider', () => ({
  httpClient: (...args) => http(...args),
}))

const review = {
  jobId: 'j1',
  album: 'Rip Album',
  albumArtist: '',
  year: '',
  tracks: [
    {
      itemId: 'a',
      position: 1,
      status: 'ready',
      path: 'Album/a.opus',
      title: 'First',
    },
    { itemId: 'b', position: 2, status: 'failed', error: 'unavailable' },
    {
      itemId: 'c',
      position: 3,
      status: 'ready',
      path: 'Album/c.opus',
      title: 'Third',
    },
  ],
}
const reply = (json) => Promise.resolve({ json })

const promoteAll = () =>
  screen.getByRole('button', { name: 'resources.rip.promote.button' })

const confirm = async () => {
  fireEvent.click(promoteAll())
  const dialog = await screen.findByRole('dialog')
  expect(dialog.textContent).toContain(
    'resources.rip.promote.confirm {"smart_count":2}',
  )
  fireEvent.click(
    within(dialog).getByRole('button', {
      name: 'resources.rip.promote.button',
    }),
  )
}

describe('PromoteButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfig.enablePromote = true
  })

  it('is not there when Promote is off', () => {
    mockConfig.enablePromote = false
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    expect(
      screen.queryByRole('button', { name: 'resources.rip.promote.button' }),
    ).toBeNull()
  })

  it('is a main action, as Save is', () => {
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    expect(promoteAll()).toHaveClass('MuiButton-containedPrimary')
  })

  it('promotes every File once confirmed, and the job leaves the list', async () => {
    const show = vi.fn()
    render(<PromoteButton jobId="j1" review={review} show={show} />)
    http.mockReturnValueOnce(
      reply({
        promoted: [
          { itemId: 'a', path: 'Vinyl Underground/A/B/01 First.opus' },
          { itemId: 'c', path: 'Vinyl Underground/A/B/03 Third.opus' },
        ],
        failed: [],
        review: null,
      }),
    )
    await confirm()

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.rip.promote.done',
        'info',
        {
          smart_count: 2,
        },
      ),
    )
    expect(http).toHaveBeenCalledWith('/api/rip/j1/promote', {
      method: 'POST',
    })
    expect(refresh).toHaveBeenCalled()
    expect(show).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('says which Files stayed in the Inbox, and why, and shows what is left', async () => {
    const show = vi.fn()
    render(<PromoteButton jobId="j1" review={review} show={show} />)
    const left = { ...review, tracks: [review.tracks[2]] }
    http.mockReturnValueOnce(
      reply({
        promoted: [
          { itemId: 'a', path: 'Vinyl Underground/A/B/01 First.opus' },
        ],
        failed: [{ itemId: 'c', error: 'the copy does not match the File' }],
        review: left,
      }),
    )
    await confirm()

    const dialog = await screen.findByRole('dialog', {
      name: 'resources.rip.promote.failedTitle',
    })
    expect(dialog.textContent).toContain(
      'resources.rip.promote.failedTrack {"position":3}',
    )
    expect(dialog.textContent).toContain('Third')
    expect(dialog.textContent).toContain('the copy does not match the File')
    expect(show).toHaveBeenCalledWith(left)
    expect(refresh).toHaveBeenCalled()
  })

  it('reports an error that stopped the whole Promote', async () => {
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    http.mockImplementationOnce(() =>
      Promise.reject({ body: { error: 'this Rip is still running' } }),
    )
    await confirm()
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'this Rip is still running',
        'warning',
      ),
    )
    // The server may have promoted some Files all the same: show what is left.
    expect(refresh).toHaveBeenCalled()
  })

  it('waits for unsaved edits on the card to be saved first', async () => {
    http.mockReturnValueOnce(reply(review))
    render(
      <ReviewCard
        record={{ id: 'j1' }}
        actions={(props) => <PromoteButton {...props} />}
      />,
    )
    await screen.findByDisplayValue('Rip Album')
    expect(promoteAll()).toBeEnabled()

    fireEvent.change(screen.getByDisplayValue('Rip Album'), {
      target: { value: 'Real Album' },
    })
    expect(promoteAll()).toBeDisabled()
  })

  it('has nothing to promote when no Track has a File', () => {
    render(
      <PromoteButton
        jobId="j1"
        review={{ ...review, tracks: [review.tracks[1]] }}
        show={vi.fn()}
      />,
    )
    expect(promoteAll()).toBeDisabled()
  })
})
