import React from 'react'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import ReviewCard from './ReviewCard.jsx'
import RipJobPanel from './RipJobPanel.jsx'

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
      artist: 'Band',
      album: 'Rip Album',
      trackNumber: '',
    },
    {
      itemId: 'b',
      position: 2,
      status: 'failed',
      error: 'ERROR: Video unavailable',
    },
    {
      itemId: 'c',
      position: 3,
      status: 'ready',
      path: 'Album/c.opus',
      title: 'Third',
      artist: 'Band',
      album: 'Rip Album',
      trackNumber: '3',
    },
  ],
}

const job = { id: 'j1', status: 'ready', folder: 'Album' }
const reply = (json) => Promise.resolve({ json })

const renderCard = async (props = {}) => {
  http.mockReturnValueOnce(reply(review))
  const utils = render(<ReviewCard record={job} {...props} />)
  await screen.findByDisplayValue('Rip Album')
  return utils
}

describe('ReviewCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("pre-fills the album and the Track list from the Files' tags", async () => {
    await renderCard()

    expect(http).toHaveBeenCalledWith('/api/rip/j1/tags')
    expect(screen.getByLabelText('resources.song.fields.title 1')).toHaveValue(
      'First',
    )
    expect(
      screen.getByLabelText('resources.song.fields.trackNumber 3'),
    ).toHaveValue('3')
    expect(screen.getByTestId('track-b').textContent).toContain(
      'ERROR: Video unavailable',
    )
  })

  it('saves only what the admin changed', async () => {
    await renderCard()
    const save = screen.getByRole('button', { name: 'ra.action.save' })
    expect(save).toBeDisabled()

    fireEvent.change(screen.getByDisplayValue('Rip Album'), {
      target: { value: 'Real Album' },
    })
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 3'), {
      target: { value: 'Third Song' },
    })
    http.mockReturnValueOnce(
      reply({
        ...review,
        album: 'Real Album',
        tracks: review.tracks.map((t) =>
          t.itemId === 'c' ? { ...t, title: 'Third Song' } : t,
        ),
      }),
    )
    fireEvent.click(save)

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith('resources.rip.review.saved', 'info'),
    )
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1/tags', {
      method: 'PUT',
      body: JSON.stringify({
        album: 'Real Album',
        tracks: [{ itemId: 'c', title: 'Third Song' }],
      }),
    })
    expect(refresh).toHaveBeenCalled()
    expect(save).toBeDisabled()
  })

  it("won't save a year that isn't a number", async () => {
    await renderCard()
    fireEvent.change(screen.getByLabelText('resources.song.fields.year'), {
      target: { value: 'soon' },
    })
    expect(
      screen.getByRole('button', { name: 'ra.action.save' }),
    ).toBeDisabled()
  })

  it("shows the server's error when saving fails", async () => {
    await renderCard()
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 1'), {
      target: { value: 'x' },
    })
    http.mockImplementationOnce(() =>
      Promise.reject({ body: { error: 'this Rip is still running' } }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'this Rip is still running',
        'warning',
      ),
    )
  })

  it('asks before discarding the whole job, and deletes nothing on cancel', async () => {
    await renderCard()

    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.discard' }),
    )
    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).toContain(
      'resources.rip.review.confirmJob {"smart_count":2}',
    )
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'ra.action.cancel' }),
    )

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(http).toHaveBeenCalledTimes(1)
  })

  it('discards the whole job once confirmed', async () => {
    await renderCard()

    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.discard' }),
    )
    const dialog = await screen.findByRole('dialog')
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.rip.review.discarded',
        'info',
      ),
    )
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1', { method: 'DELETE' })
    expect(refresh).toHaveBeenCalled()
  })

  it('discards a single Track once confirmed, and drops it from the card', async () => {
    await renderCard()

    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.rip.review.discardTrack 3',
      }),
    )
    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).toContain('"title":"Third"')
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )

    await waitFor(() => expect(screen.queryByTestId('track-c')).toBeNull())
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1/items/c', {
      method: 'DELETE',
    })
    expect(screen.getByTestId('track-a')).toBeInTheDocument()
  })

  it('keeps unsaved edits to other Tracks when one is discarded', async () => {
    await renderCard()
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 1'), {
      target: { value: 'Edited' },
    })
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.rip.review.discardTrack 3',
      }),
    )
    const dialog = await screen.findByRole('dialog')
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )

    await waitFor(() => expect(screen.queryByTestId('track-c')).toBeNull())
    expect(screen.getByLabelText('resources.song.fields.title 1')).toHaveValue(
      'Edited',
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(
      screen.getByRole('button', { name: 'ra.action.save' }),
    ).not.toBeDisabled()
  })

  it('still offers Discard when the tags cannot be read', async () => {
    http.mockImplementationOnce(() =>
      Promise.reject({ body: { error: 'the Inbox is not set up' } }),
    )
    render(
      <ReviewCard record={{ ...job, items: [{ id: 'a', path: 'a.opus' }] }} />,
    )
    expect(
      await screen.findByText(/resources.rip.review.loadError/),
    ).toBeInTheDocument()
    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.discard' }),
    )
    expect((await screen.findByRole('dialog')).textContent).toContain(
      'resources.rip.review.confirmJob {"smart_count":1}',
    )
  })

  it('has a slot on each Track row, and room for more actions', async () => {
    await renderCard({
      trackExtra: (t) => <span data-testid={`extra-${t.itemId}`} />,
      actions: <button>Promote all</button>,
    })
    expect(screen.getByTestId('extra-a')).toBeInTheDocument()
    expect(screen.getByTestId('extra-c')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Promote all' }),
    ).toBeInTheDocument()
  })
})

describe('RipJobPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the review card for a ready job', async () => {
    http.mockReturnValueOnce(reply(review))
    render(<RipJobPanel record={job} fallback={<div data-testid="items" />} />)
    expect(await screen.findByDisplayValue('Rip Album')).toBeInTheDocument()
    expect(screen.queryByTestId('items')).toBeNull()
  })

  it('offers Discard for a failed job, under its item list', async () => {
    render(
      <RipJobPanel
        record={{ ...job, status: 'failed', items: [] }}
        fallback={<div data-testid="items" />}
      />,
    )
    expect(screen.getByTestId('items')).toBeInTheDocument()
    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.discard' }),
    )
    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).toContain('resources.rip.review.confirmNoFiles')
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )
    await waitFor(() =>
      expect(http).toHaveBeenCalledWith('/api/rip/j1', { method: 'DELETE' }),
    )
  })

  it('shows the item list while the job is still running, without loading tags', () => {
    render(
      <RipJobPanel
        record={{ ...job, status: 'downloading' }}
        fallback={<div data-testid="items" />}
      />,
    )
    expect(screen.getByTestId('items')).toBeInTheDocument()
    expect(http).not.toHaveBeenCalled()
  })
})
