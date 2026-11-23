import React from 'react'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import ReviewCard, { PROPOSALS_POLL_MS } from './ReviewCard.jsx'
import { clearAllDrafts } from './reviewDrafts'

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

const track = (itemId, position, title, proposal) => ({
  itemId,
  position,
  status: 'ready',
  path: `Album/${itemId}.opus`,
  title,
  artist: 'BandVEVO',
  album: 'Rip Album',
  trackNumber: '',
  ...(proposal ? { proposal } : {}),
})

// A card whose Files still have their YouTube tags, with proposals worked out for them.
const proposed = {
  jobId: 'j1',
  album: 'Rip Album',
  albumArtist: '',
  year: '',
  proposal: { album: 'Real Album', albumArtist: 'Band', year: '1999' },
  proposalsPending: false,
  tracks: [
    track('a', 1, 'Band - First (Official Video)', {
      status: 'ready',
      title: 'First',
      artist: 'Band',
      albumFrom: 'musicbrainz',
    }),
    track('c', 2, 'Band - Second (Lyrics)', {
      status: 'ready',
      title: 'Second',
      artist: 'Band',
      albumFrom: 'musicbrainz',
      note: 'MusicBrainz couldn’t be asked: busy',
    }),
  ],
}

const pending = {
  ...proposed,
  proposal: undefined,
  proposalsPending: true,
  tracks: proposed.tracks.map((t) => ({
    ...t,
    proposal: { status: 'pending' },
  })),
}

const job = { id: 'j1', status: 'ready', folder: 'Album' }
const reply = (json) => Promise.resolve({ json })

const renderCard = async (rev = proposed) => {
  http.mockReturnValueOnce(reply(rev))
  render(<ReviewCard record={job} />)
  await screen.findByDisplayValue('Rip Album')
}

const field = (label) => screen.getByLabelText(label)
const offer = (value) =>
  screen.getByRole('button', {
    name: `resources.rip.review.proposed {"value":"${value}"}`,
  })

describe('ReviewCard proposals (#34)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllDrafts()
  })

  it("offers the proposals beside the Files' own tags, and writes nothing", async () => {
    await renderCard()

    // The fields still hold the Files' tags; each proposal is offered under its field.
    expect(field('resources.song.fields.album')).toHaveValue('Rip Album')
    expect(field('resources.song.fields.title 1')).toHaveValue(
      'Band - First (Official Video)',
    )
    expect(offer('Real Album')).toBeInTheDocument()
    expect(offer('1999')).toBeInTheDocument()
    expect(offer('First')).toBeInTheDocument()
    expect(screen.getByText(/busy/)).toBeInTheDocument()

    expect(
      screen.getByRole('button', { name: 'ra.action.save' }),
    ).toBeDisabled()
    expect(http).toHaveBeenCalledTimes(1) // the GET only
  })

  it('takes one proposal into its field', async () => {
    await renderCard()
    fireEvent.click(offer('Real Album'))

    expect(field('resources.song.fields.album')).toHaveValue('Real Album')
    expect(field('resources.song.fields.year')).toHaveValue('')
    // Marked as proposed, with the File's own tag, until it is saved.
    expect(
      screen.getByText(
        'resources.rip.review.proposedFile {"file":"Rip Album"}',
      ),
    ).toBeInTheDocument()
    expect(http).toHaveBeenCalledTimes(1)
  })

  it('takes them all with "Use proposals", and only Save writes them', async () => {
    await renderCard()
    fireEvent.click(
      screen.getByRole('button', { name: 'resources.rip.review.useProposals' }),
    )
    expect(field('resources.song.fields.album')).toHaveValue('Real Album')
    expect(field('resources.song.fields.albumArtist')).toHaveValue('Band')
    expect(field('resources.song.fields.year')).toHaveValue('1999')
    expect(field('resources.song.fields.title 2')).toHaveValue('Second')
    expect(field('resources.song.fields.artist 2')).toHaveValue('Band')
    expect(http).toHaveBeenCalledTimes(1)

    http.mockReturnValueOnce(reply(proposed))
    fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))
    await waitFor(() => expect(http).toHaveBeenCalledTimes(2))
    const [url, opts] = http.mock.calls[1]
    expect(url).toBe('/api/rip/j1/tags')
    expect(opts.method).toBe('PUT')
    expect(JSON.parse(opts.body)).toEqual({
      album: 'Real Album',
      albumArtist: 'Band',
      year: '1999',
      tracks: [
        { itemId: 'a', title: 'First', artist: 'Band' },
        { itemId: 'c', title: 'Second', artist: 'Band' },
      ],
    })
  })

  it('keeps Track rows the same height, with or without a proposal under a field', async () => {
    // The first Track has proposals; the second has none.
    await renderCard({
      ...proposed,
      tracks: [
        proposed.tracks[0],
        { ...track('c', 2, 'Second', undefined), artist: 'Band' },
      ],
    })
    // Every Track field keeps a line for a hint, filled or not, and a hint never wraps.
    for (const label of [
      'resources.song.fields.title 1',
      'resources.song.fields.title 2',
      'resources.song.fields.artist 2',
      'resources.song.fields.trackNumber 2',
    ]) {
      const hint = field(label)
        .closest('.MuiFormControl-root')
        .querySelector('.MuiFormHelperText-root')
      expect(hint).not.toBeNull()
      expect(getComputedStyle(hint).whiteSpace).toBe('nowrap')
    }
  })

  it('offers "Use proposals" and "Refresh proposals" as outlined buttons', async () => {
    await renderCard()
    for (const name of [
      'resources.rip.review.useProposals',
      'resources.rip.review.refreshProposals',
    ]) {
      expect(screen.getByRole('button', { name })).toHaveClass(
        'MuiButton-outlined',
      )
    }
  })

  it('has no proposals bar on a card without proposals', async () => {
    await renderCard({
      ...proposed,
      proposal: undefined,
      tracks: proposed.tracks.map(({ proposal, ...t }) => t),
    })
    expect(
      screen.queryByRole('button', {
        name: 'resources.rip.review.useProposals',
      }),
    ).toBeNull()
  })

  describe('while proposals are worked out', () => {
    beforeEach(() => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
    })
    afterEach(() => {
      vi.useRealTimers()
    })

    it('says so, and shows them once they are there, keeping the edits made meanwhile', async () => {
      await renderCard(pending)
      expect(
        screen.getByText('resources.rip.review.proposalsPending'),
      ).toBeInTheDocument()
      fireEvent.change(field('resources.song.fields.year'), {
        target: { value: '2001' },
      })

      http.mockReturnValueOnce(reply(proposed))
      await act(() => vi.advanceTimersByTimeAsync(PROPOSALS_POLL_MS))

      expect(
        await screen.findByRole('button', {
          name: 'resources.rip.review.proposed {"value":"Real Album"}',
        }),
      ).toBeInTheDocument()
      expect(http).toHaveBeenLastCalledWith('/api/rip/j1/tags')
      expect(field('resources.song.fields.year')).toHaveValue('2001')
      expect(
        screen.queryByText('resources.rip.review.proposalsPending'),
      ).toBeNull()
    })

    it('keeps the edits when proposals arrive after the list refreshed', async () => {
      http.mockReturnValueOnce(reply(pending))
      const { rerender } = render(<ReviewCard key={0} record={job} />)
      await screen.findByDisplayValue('Rip Album')
      fireEvent.change(field('resources.song.fields.year'), {
        target: { value: '2001' },
      })

      rerender(<ReviewCard key={1} record={job} />)
      expect(await screen.findByDisplayValue('2001')).toBeInTheDocument()
      http.mockReturnValueOnce(reply(proposed))
      await act(() => vi.advanceTimersByTimeAsync(PROPOSALS_POLL_MS))

      expect(
        await screen.findByRole('button', {
          name: 'resources.rip.review.proposed {"value":"Real Album"}',
        }),
      ).toBeInTheDocument()
      expect(field('resources.song.fields.year')).toHaveValue('2001')
      expect(http).toHaveBeenCalledTimes(2) // the first GET, then the look for proposals
    })

    it('works them out again on "Refresh proposals"', async () => {
      await renderCard()
      http.mockReturnValueOnce(reply(pending))
      fireEvent.click(
        screen.getByRole('button', {
          name: 'resources.rip.review.refreshProposals',
        }),
      )
      await waitFor(() =>
        expect(http).toHaveBeenCalledWith('/api/rip/j1/proposals', {
          method: 'POST',
        }),
      )
      expect(
        await screen.findByText('resources.rip.review.proposalsPending'),
      ).toBeInTheDocument()
    })
  })
})
