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

const track = (itemId, position, albumId) => ({
  itemId,
  position,
  status: 'ready',
  path: `Album/${itemId}.opus`,
  mediaFileId: `mf-${itemId}`,
  ...(albumId ? { albumId } : {}),
  title: `Track ${position}`,
  artist: 'Band',
  album: 'Rip Album',
  trackNumber: '',
})

const card = (cover, albums = ['al1', 'al1']) => ({
  jobId: 'j1',
  album: 'Rip Album',
  albumArtist: '',
  year: '',
  tracks: albums.map((al, i) => track(`t${i}`, i + 1, al)),
  ...(cover ? { cover } : {}),
})

const withCover = card({ albumId: 'al1', artworkId: 'al-al1_0123456789abcdef' })

const job = { id: 'j1', status: 'ready', folder: 'Album' }
const reply = (json) => Promise.resolve({ json })

const renderCard = async (rev) => {
  http.mockReturnValueOnce(reply(rev))
  render(<ReviewCard record={job} />)
  await screen.findByDisplayValue('Rip Album')
}

const cover = () =>
  screen.queryByRole('img', { name: 'resources.rip.review.cover' })
const noCover = () => screen.queryByLabelText('resources.rip.review.noCover')
const coverParams = () => new URL(cover().src, 'http://localhost').searchParams

describe('ReviewCard cover', () => {
  let now
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllDrafts()
    // The cover's URL asks getCoverArt as the signed-in user.
    localStorage.setItem('username', 'admin')
    localStorage.setItem('subsonic-token', 'token')
    localStorage.setItem('subsonic-salt', 'salt')
    now = 1000
    vi.spyOn(Date, 'now').mockImplementation(() => now)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
  })

  it("shows the album's cover from getCoverArt, as a small thumbnail", async () => {
    await renderCard(withCover)

    expect(cover()).toBeInTheDocument()
    expect(cover().src).toContain('/rest/getCoverArt?')
    expect(coverParams().get('id')).toBe('al-al1_0123456789abcdef')
    expect(coverParams().get('size')).toBe('128')
    expect(noCover()).not.toBeInTheDocument()
  })

  it('shows a placeholder for an album known to have no cover', async () => {
    await renderCard(
      card({ albumId: 'al1', artworkId: 'al-al1', absent: true }),
    )

    expect(noCover()).toBeInTheDocument()
    expect(cover()).not.toBeInTheDocument()
  })

  it('shows a placeholder, not a broken image, when the cover fails to load', async () => {
    await renderCard(withCover)

    fireEvent.error(cover())

    expect(noCover()).toBeInTheDocument()
    expect(cover()).not.toBeInTheDocument()
  })

  it('shows a placeholder when the Tracks belong to several albums', async () => {
    await renderCard(card(undefined, ['al1', 'al2']))

    expect(noCover()).toBeInTheDocument()
    expect(cover()).not.toBeInTheDocument()
  })

  it('shows a placeholder before the Inbox has been scanned', async () => {
    await renderCard(card(undefined, [undefined]))

    expect(noCover()).toBeInTheDocument()
  })

  it('asks for the cover afresh after a Save, so a new one shows', async () => {
    await renderCard(withCover)
    const before = coverParams().get('_')

    fireEvent.change(screen.getByDisplayValue('Rip Album'), {
      target: { value: 'Real Album' },
    })
    now = 2000
    http.mockReturnValueOnce(
      reply({ ...withCover, album: 'Real Album', tracks: withCover.tracks }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))
    await screen.findByDisplayValue('Real Album')

    await waitFor(() => expect(coverParams().get('_')).not.toBe(before))
    expect(coverParams().get('id')).toBe('al-al1_0123456789abcdef')
  })

  it('takes in a new cover after a reload, even once a failed one showed the placeholder', async () => {
    // Refresh proposals shows on a card with proposals.
    const proposing = (rev) => ({
      ...rev,
      tracks: rev.tracks.map((t) => ({
        ...t,
        proposal: { status: 'ready', title: t.title },
      })),
    })
    await renderCard(proposing(withCover))
    fireEvent.error(cover())
    expect(noCover()).toBeInTheDocument()

    now = 2000
    http.mockReturnValueOnce(
      reply(
        proposing(
          card({ albumId: 'al1', artworkId: 'al-al1_fedcba9876543210' }),
        ),
      ),
    )
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.rip.review.refreshProposals',
      }),
    )

    await waitFor(() => expect(cover()).toBeInTheDocument())
    expect(coverParams().get('id')).toBe('al-al1_fedcba9876543210')
    expect(coverParams().get('_')).toBe('2000')
  })
  it('shows the cover once the Inbox scan finds the album, while proposals are worked out', async () => {
    vi.useFakeTimers({
      shouldAdvanceTime: true,
      toFake: ['setTimeout', 'clearTimeout'],
    })
    try {
      const pending = (rev) => ({
        ...rev,
        proposalsPending: true,
        tracks: rev.tracks.map((t) => ({
          ...t,
          proposal: { status: 'pending' },
        })),
      })
      await renderCard(pending(card(undefined, [undefined])))
      expect(noCover()).toBeInTheDocument()

      http.mockReturnValueOnce(
        reply(pending(card({ albumId: 'al1', artworkId: 'al-al1' }, ['al1']))),
      )
      await act(() => vi.advanceTimersByTimeAsync(PROPOSALS_POLL_MS))

      await waitFor(() => expect(cover()).toBeInTheDocument())
      expect(coverParams().get('id')).toBe('al-al1')
    } finally {
      vi.useRealTimers()
    }
  })
})
