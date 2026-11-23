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

// The plan Promote shows (#171): Organize's, from the Inbox into the Library.
const plan = {
  token: 'tok-1',
  tracks: 1,
  files: 0,
  inPlace: 0,
  unplaced: { noTrackNumber: 1 },
  moves: [
    {
      trackId: 't1',
      path: 'Album/a.opus',
      newPath: 'Rip Artist/Rip Album (2024)/01 First.opus',
      newLibraryId: 1,
      status: 'move',
    },
  ],
  left: [{ trackId: 't3', path: 'Album/c.opus', status: 'noTrackNumber' }],
  totalMoves: 1,
  totalLeft: 1,
}
const reply = (json, status = 200) => Promise.resolve({ json, status })

const promoteAll = () =>
  screen.getByRole('button', { name: 'resources.rip.promote.button' })

describe('PromoteButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfig.enablePromote = true
  })

  it('is not there when Promote is off (the Library is read-only)', () => {
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

  it('shows where each File lands in the Library before anything moves, and why any stays', async () => {
    http.mockReturnValueOnce(reply(plan))
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    fireEvent.click(promoteAll())

    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText('Album/a.opus')
    expect(
      within(dialog).getByText('→ Rip Artist/Rip Album (2024)/01 First.opus'),
    ).toBeInTheDocument()
    expect(within(dialog).getByText('Album/c.opus')).toBeInTheDocument()
    expect(dialog.textContent).toContain(
      'resources.organize.reasons.noTrackNumber',
    )
    expect(dialog.textContent).toContain('resources.rip.promote.help')
    expect(http).toHaveBeenCalledTimes(1)
    expect(http).toHaveBeenCalledWith(
      '/api/rip/j1/promote/plan?_start=0&_end=100',
      { method: 'POST' },
    )
  })

  it('runs the plan shown, by its token, and the list refreshes', async () => {
    http.mockReturnValueOnce(reply(plan))
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    fireEvent.click(promoteAll())
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText('Album/a.opus')

    http.mockReturnValueOnce(reply({ id: 'job-1', state: 'done', skipped: 0 }))
    fireEvent.click(within(dialog).getByTestId('organize-run'))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1/promote', {
      method: 'POST',
      body: JSON.stringify({ token: 'tok-1' }),
    })
    expect(notify).toHaveBeenCalledWith('resources.organize.done', {
      type: 'info',
    })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('says when the job runs on in the background', async () => {
    http.mockReturnValueOnce(reply(plan))
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    fireEvent.click(promoteAll())
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText('Album/a.opus')
    http.mockReturnValueOnce(reply({ id: 'job-1', state: 'running' }, 202))
    fireEvent.click(within(dialog).getByTestId('organize-run'))
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith('resources.organize.inBackground', {
        type: 'info',
      }),
    )
  })

  it('reports why there is nothing to promote yet', async () => {
    http.mockImplementationOnce(() =>
      Promise.reject({ body: { error: 'nothingToPromote' } }),
    )
    render(<PromoteButton jobId="j1" review={review} show={vi.fn()} />)
    fireEvent.click(promoteAll())
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.organize.errors.nothingToPromote',
        { type: 'warning' },
      ),
    )
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
