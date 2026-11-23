import React from 'react'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import ReviewCard from './ReviewCard.jsx'
import RipJobPanel from './RipJobPanel.jsx'
import { clearAllDrafts } from './reviewDrafts'
import { stubResizeObserver } from '../../common/stubResizeObserver'

// RipJobPanel cuts the job's origin short with Clipped, which watches its size.
let restoreResizeObserver
beforeEach(() => {
  restoreResizeObserver = stubResizeObserver().restore
})
afterEach(() => restoreResizeObserver())

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

// destructive says whether a button has Discard's red, the theme's error colour (useDiscardStyles).
// jsdom doesn't apply the colour itself, so this looks for the styles' class.
const destructive = (button) => /\bNDRipDiscard-/.test(button.className)

const renderCard = async (props = {}) => {
  http.mockReturnValueOnce(reply(review))
  const utils = render(<ReviewCard record={job} {...props} />)
  await screen.findByDisplayValue('Rip Album')
  return utils
}

describe('ReviewCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllDrafts()
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
    // The list isn't refreshed, so other cards are left as they are.
    expect(refresh).not.toHaveBeenCalled()
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

// The review card's look (#190): one header row of album fields, fixed Track columns, every field
// outlined, and Save, Promote all and Discard in a footer.
describe("ReviewCard's layout", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllDrafts()
  })

  it('puts Album, Album artist and Year in one header row', async () => {
    await renderCard()
    const header = screen.getByTestId('review-header')
    for (const label of [
      'resources.song.fields.album',
      'resources.song.fields.albumArtist',
      'resources.song.fields.year',
    ]) {
      expect(header).toContainElement(screen.getByLabelText(label))
    }
  })

  it('gives the Track number a narrow fixed column, and Title more room than Artist', async () => {
    await renderCard()
    const table = screen.getByTestId('review-tracks')
    expect(table.style.tableLayout).toBe('fixed')
    const width = (name) =>
      table.querySelector(`col[data-column="${name}"]`).style.width
    expect(width('trackNumber')).toMatch(/^\d+(\.\d+)?em$/)
    expect(parseFloat(width('title'))).toBeGreaterThan(
      parseFloat(width('artist')),
    )
  })

  it('outlines every Track field, like the album fields', async () => {
    await renderCard()
    for (const label of [
      'resources.song.fields.album',
      'resources.song.fields.trackNumber 1',
      'resources.song.fields.title 1',
      'resources.song.fields.artist 3',
    ]) {
      expect(
        screen.getByLabelText(label).closest('.MuiOutlinedInput-root'),
      ).not.toBeNull()
    }
  })

  it('puts Save, the other actions and Discard in its footer', async () => {
    await renderCard({ actions: <button>Promote all</button> })
    const footer = screen.getByTestId('review-footer')
    const save = within(footer).getByRole('button', { name: 'ra.action.save' })
    expect(save).toHaveClass('MuiButton-containedPrimary')
    expect(
      within(footer).getByRole('button', { name: 'Promote all' }),
    ).toBeInTheDocument()
    const discard = within(footer).getByRole('button', {
      name: 'resources.rip.review.discard',
    })
    // The same size as the page's other buttons.
    expect(save).toHaveClass('MuiButton-sizeSmall')
    expect(discard).toHaveClass('MuiButton-sizeSmall')
  })

  it('marks Discard, for the job and for a Track, in red', async () => {
    await renderCard()
    const discard = screen.getByRole('button', {
      name: 'resources.rip.review.discard',
    })
    expect(discard).toHaveClass('MuiButton-outlined')
    expect(destructive(discard)).toBe(true)
    expect(
      destructive(
        screen.getByRole('button', {
          name: 'resources.rip.review.discardTrack 1',
        }),
      ),
    ).toBe(true)

    fireEvent.click(discard)
    const dialog = await screen.findByRole('dialog')
    const confirm = within(dialog).getByRole('button', {
      name: 'resources.rip.review.discard',
    })
    expect(confirm).toHaveClass('MuiButton-contained')
    expect(destructive(confirm)).toBe(true)
  })
})

// The Rip list remounts every expanded card on each refresh (react-admin 3 keys the list's content
// on its refresh counter): a new key does the same here.
describe('ReviewCard across refreshes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    http.mockReset()
    clearAllDrafts()
  })

  it('keeps unsaved edits when the list refreshes', async () => {
    http.mockReturnValueOnce(reply(review))
    const { rerender } = render(<ReviewCard key={0} record={job} />)
    await screen.findByDisplayValue('Rip Album')
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 1'), {
      target: { value: 'Edited' },
    })

    http.mockReturnValue(reply(review))
    rerender(<ReviewCard key={1} record={job} />)

    expect(
      await screen.findByLabelText('resources.song.fields.title 1'),
    ).toHaveValue('Edited')
    expect(
      screen.getByRole('button', { name: 'ra.action.save' }),
    ).not.toBeDisabled()
    // The card came back from its draft, without reloading the tags over the edits.
    expect(http).toHaveBeenCalledTimes(1)
  })

  it("keeps other cards' unsaved edits when one is saved, and leaves none behind", async () => {
    const other = { ...review, jobId: 'j2', album: 'Other Album' }
    http.mockReturnValueOnce(reply(review)).mockReturnValueOnce(reply(other))
    const cards = (key) => (
      <>
        <div data-testid="card-j1">
          <ReviewCard key={key} record={job} />
        </div>
        <div data-testid="card-j2">
          <ReviewCard key={key} record={{ ...job, id: 'j2' }} />
        </div>
      </>
    )
    const { rerender } = render(cards(0))
    const j1 = within(await screen.findByTestId('card-j1'))
    const j2 = within(screen.getByTestId('card-j2'))
    await j2.findByDisplayValue('Other Album')
    fireEvent.change(j1.getByDisplayValue('Rip Album'), {
      target: { value: 'Saved Album' },
    })
    fireEvent.change(j2.getByDisplayValue('Other Album'), {
      target: { value: 'Unsaved Album' },
    })

    http.mockReturnValueOnce(reply({ ...review, album: 'Saved Album' }))
    fireEvent.click(j1.getByRole('button', { name: 'ra.action.save' }))
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith('resources.rip.review.saved', 'info'),
    )
    expect(refresh).not.toHaveBeenCalled()
    expect(j2.getByDisplayValue('Unsaved Album')).toBeInTheDocument()

    // After a refresh, the saved card loads the saved tags, and the other keeps its edits.
    http.mockReturnValueOnce(reply({ ...review, album: 'Saved Album' }))
    rerender(cards(1))
    expect(await j1.findByDisplayValue('Saved Album')).toBeInTheDocument()
    expect(j1.getByRole('button', { name: 'ra.action.save' })).toBeDisabled()
    expect(j2.getByDisplayValue('Unsaved Album')).toBeInTheDocument()
    expect(
      j2.getByRole('button', { name: 'ra.action.save' }),
    ).not.toBeDisabled()
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1/tags')
  })

  it('shows a Save still in flight when the list refreshes', async () => {
    http.mockReturnValueOnce(reply(review))
    const { rerender } = render(<ReviewCard key={0} record={job} />)
    fireEvent.change(await screen.findByDisplayValue('Rip Album'), {
      target: { value: 'Saved Album' },
    })
    let saved
    http.mockReturnValueOnce(
      new Promise((resolve) => {
        saved = resolve
      }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))

    rerender(<ReviewCard key={1} record={job} />)
    expect(
      await screen.findByRole('button', {
        name: 'resources.rip.review.saving',
      }),
    ).toBeDisabled()
    saved({ json: { ...review, album: 'Saved Album' } })

    expect(await screen.findByDisplayValue('Saved Album')).toBeInTheDocument()
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'ra.action.save' }),
      ).toBeDisabled(),
    )
    expect(http).toHaveBeenCalledTimes(2) // the GET and the PUT
  })

  it("drops a discarded Track's unsaved edits, keeping the others'", async () => {
    http.mockReturnValueOnce(reply(review))
    const { rerender } = render(<ReviewCard key={0} record={job} />)
    await screen.findByDisplayValue('Rip Album')
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 1'), {
      target: { value: 'Edited' },
    })
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 3'), {
      target: { value: 'Gone' },
    })
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.rip.review.discardTrack 3',
      }),
    )
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )
    await waitFor(() => expect(screen.queryByTestId('track-c')).toBeNull())

    rerender(<ReviewCard key={1} record={job} />)
    expect(
      await screen.findByLabelText('resources.song.fields.title 1'),
    ).toHaveValue('Edited')
    expect(screen.queryByTestId('track-c')).toBeNull()
    expect(http).toHaveBeenCalledTimes(2)
  })

  it('drops the draft when the only edited Track is discarded', async () => {
    http.mockReturnValueOnce(reply(review))
    const { rerender } = render(<ReviewCard key={0} record={job} />)
    await screen.findByDisplayValue('Rip Album')
    fireEvent.change(screen.getByLabelText('resources.song.fields.title 3'), {
      target: { value: 'Gone' },
    })
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.rip.review.discardTrack 3',
      }),
    )
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )
    await waitFor(() => expect(screen.queryByTestId('track-c')).toBeNull())

    const fresh = {
      ...review,
      tracks: review.tracks.filter((t) => t.itemId !== 'c'),
    }
    http.mockReturnValueOnce(reply(fresh))
    rerender(<ReviewCard key={1} record={job} />)
    await waitFor(() =>
      expect(http).toHaveBeenLastCalledWith('/api/rip/j1/tags'),
    )
    expect(http).toHaveBeenCalledTimes(3)
  })

  it("drops a discarded job's unsaved edits", async () => {
    http.mockReturnValueOnce(reply(review))
    const { rerender } = render(<ReviewCard key={0} record={job} />)
    fireEvent.change(await screen.findByDisplayValue('Rip Album'), {
      target: { value: 'Edited' },
    })
    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.discard' }),
    )
    http.mockReturnValueOnce(reply(null))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: 'resources.rip.review.discard',
      }),
    )
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.rip.review.discarded',
        'info',
      ),
    )

    // Were the job to come back (a new Inbox scan, say), its card starts afresh.
    http.mockReturnValueOnce(reply(review))
    rerender(<ReviewCard key={1} record={job} />)
    expect(await screen.findByDisplayValue('Rip Album')).toBeInTheDocument()
    expect(http).toHaveBeenLastCalledWith('/api/rip/j1/tags')
  })
})

describe('RipJobPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllDrafts()
  })

  it('shows the review card for a ready job', async () => {
    http.mockReturnValueOnce(reply(review))
    render(<RipJobPanel record={job} fallback={<div data-testid="items" />} />)
    expect(await screen.findByDisplayValue('Rip Album')).toBeInTheDocument()
    expect(screen.queryByTestId('items')).toBeNull()
  })

  it("cuts a long URL short above the card, so it can't widen the job table", async () => {
    http.mockReturnValueOnce(reply(review))
    const url = 'https://www.youtube.com/playlist?list=' + 'x'.repeat(200)
    render(<RipJobPanel record={{ ...job, kind: 'rip', url }} />)
    await screen.findByDisplayValue('Rip Album')
    const link = within(screen.getByTestId('job-origin')).getByRole('link')
    expect(link.textContent).toBe(url)
    expect(link.closest('[class*="NDClipped-clipped"]')).not.toBeNull()
  })

  it('offers Discard for a failed job, under its item list', async () => {
    render(
      <RipJobPanel
        record={{ ...job, status: 'failed', items: [] }}
        fallback={<div data-testid="items" />}
      />,
    )
    expect(screen.getByTestId('items')).toBeInTheDocument()
    const discard = screen.getByRole('button', {
      name: 'resources.rip.review.discard',
    })
    expect(destructive(discard)).toBe(true)
    fireEvent.click(discard)
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
