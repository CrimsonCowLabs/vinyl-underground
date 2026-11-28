import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import PromoteButton from './PromoteButton.jsx'
import ReviewCard from './ReviewCard.jsx'

const { mockConfig, mockDataProvider } = vi.hoisted(() => ({
  mockConfig: { enablePromote: true },
  mockDataProvider: {
    correctAlbum: vi.fn(),
    findMusicBrainzReleases: vi.fn(),
    matchMusicBrainzRelease: vi.fn(),
    fitMusicBrainzReleases: vi.fn(),
  },
}))
vi.mock('../../config', () => ({ default: mockConfig }))

const notify = vi.fn()
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
    useRefresh: () => vi.fn(),
    useDataProvider: () => mockDataProvider,
  }
})

vi.mock('../../dataProvider', () => ({
  httpClient: (...args) => http(...args),
}))

// A card whose Tracks are one album, scanned from the Inbox.
const review = (jobId, albumId = 'al-1', other = albumId) => ({
  jobId,
  album: 'Live',
  albumArtist: 'Grateful Dead',
  year: '1977',
  tracks: [
    {
      itemId: 'a',
      position: 1,
      status: 'ready',
      path: 'Live/01.mp3',
      title: 'One',
      artist: 'Grateful Dead',
      mediaFileId: 'mf-1',
      albumId,
    },
    { itemId: 'b', position: 2, status: 'failed', error: 'unavailable' },
    {
      itemId: 'c',
      position: 3,
      status: 'ready',
      path: 'Live/02.mp3',
      title: 'Two',
      artist: 'Grateful Dead',
      mediaFileId: 'mf-2',
      albumId: other,
    },
  ],
})

const release = {
  id: 'rel-1',
  releaseGroupId: 'rg-1',
  title: 'Live',
  artist: 'Grateful Dead',
  year: '1977',
  description: 'Live (US, LP, 1977)',
}
const match = {
  release: { id: 'rel-1', albumArtist: 'Grateful Dead' },
  holds: true,
  tracks: [
    {
      id: 'mf-1',
      title: 'One',
      disc: 1,
      track: 1,
      paired: { id: 'rt-1', title: 'One', disc: 1, position: 1 },
    },
    {
      id: 'mf-2',
      title: 'Two',
      disc: 1,
      track: 2,
      paired: { id: 'rt-2', title: 'Two', disc: 1, position: 2 },
    },
  ],
  unpaired: [],
  albumArtistMatches: true,
  canonical: { album: {}, albumArtist: {}, date: {}, titles: 0 },
  genres: ['Rock'],
}

const reply = (json, status = 200) => Promise.resolve({ json, status })

let jobs = 0
// card shows a fresh job's card, with Promote among its actions, once its review is in.
const card = async (rev) => {
  const jobId = `job-${++jobs}`
  const r = rev(jobId)
  http.mockReturnValueOnce(reply(r))
  render(
    <ReviewCard
      record={{ id: jobId }}
      actions={(props) => <PromoteButton {...props} />}
    />,
  )
  await screen.findAllByDisplayValue('Live')
  return jobId
}
const find = () =>
  screen.queryByRole('button', { name: 'resources.album.musicbrainz.find' })
const applyButton = () =>
  screen.getByRole('button', { name: 'resources.rip.review.release.apply' })
const promoteAll = () =>
  screen.getByRole('button', { name: 'resources.rip.promote.button' })
const pick = async () => {
  fireEvent.click(find())
  fireEvent.click(await screen.findByTestId('mbz-release-rel-1'))
  await screen.findByTestId('mbz-pairing')
}

describe('Find on MusicBrainz on a review card', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfig.enablePromote = true
    mockDataProvider.findMusicBrainzReleases.mockResolvedValue({
      data: [release],
    })
    mockDataProvider.fitMusicBrainzReleases.mockResolvedValue({ data: [] })
    mockDataProvider.matchMusicBrainzRelease.mockResolvedValue({ data: match })
    mockDataProvider.correctAlbum.mockResolvedValue({
      data: { done: true, albumId: 'al-2' },
    })
  })

  it("is there for a card whose Tracks are one album, searching by the card's album, album artist and Track count", async () => {
    await card((id) => review(id))
    await pick()
    expect(mockDataProvider.findMusicBrainzReleases).toHaveBeenCalledWith(
      'Grateful Dead',
      'Live',
      false,
    )
    expect(mockDataProvider.matchMusicBrainzRelease).toHaveBeenCalledWith(
      'rel-1',
      'al-1',
    )
  })

  it('is not there for a card mixing albums', async () => {
    await card((id) => review(id, 'al-1', 'al-9'))
    expect(find()).toBeNull()
  })

  it('is not there before the Files are scanned', async () => {
    await card((id) => {
      const r = review(id)
      r.tracks[0].albumId = undefined
      return r
    })
    expect(find()).toBeNull()
  })

  it('is not there while editing is off', async () => {
    mockConfig.enablePromote = false
    await card((id) => review(id))
    expect(find()).toBeNull()
  })

  it('applies the paired release with what the admin ticked, then reloads the card, which may be on a new album', async () => {
    const jobId = await card((id) => review(id))
    await pick()
    expect(applyButton()).toBeEnabled()
    fireEvent.click(screen.getByTestId('mbz-option-genres'))
    const reloaded = review(jobId, 'al-2')
    reloaded.year = '1977-05-08'
    http.mockReturnValueOnce(reply(reloaded))
    fireEvent.click(applyButton())

    await screen.findByDisplayValue('1977-05-08')
    expect(mockDataProvider.correctAlbum).toHaveBeenCalledWith('al-1', {
      mbzAlbumId: 'rel-1',
      canonical: {
        album: false,
        albumArtist: false,
        date: false,
        titles: false,
        artists: false,
      },
      replaceTrackIds: false,
      cover: true,
      genres: false,
    })
    expect(http).toHaveBeenLastCalledWith(`/api/rip/${jobId}/tags`)
    expect(notify).toHaveBeenCalledWith(
      'resources.rip.review.release.applied',
      {
        type: 'info',
      },
    )
    // The card is on the new album, with no release picked: Find starts over for it.
    expect(screen.queryByTestId('mbz-pairing')).toBeNull()
    fireEvent.click(find())
    fireEvent.click(await screen.findByTestId('mbz-release-rel-1'))
    await waitFor(() =>
      expect(mockDataProvider.matchMusicBrainzRelease).toHaveBeenLastCalledWith(
        'rel-1',
        'al-2',
      ),
    )
  })

  it("says why the server refused, as when the card's Files are being promoted", async () => {
    mockDataProvider.correctAlbum.mockRejectedValue({
      body: { error: 'promoting' },
    })
    await card((id) => review(id))
    await pick()
    fireEvent.click(applyButton())
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.rip.review.release.errors.promoting',
        { type: 'warning' },
      ),
    )
    expect(applyButton()).toBeEnabled()
  })

  it('waits for unsaved edits to be saved first', async () => {
    const jobId = await card((id) => review(id))
    fireEvent.change(document.getElementById(`rip-${jobId}-album`), {
      target: { value: 'Live!' },
    })
    expect(find()).toBeDisabled()
  })

  it('is disabled while Promote is under way, and Promote while a release is picked or applied', async () => {
    await card((id) => review(id))
    expect(promoteAll()).toBeEnabled()
    await pick()
    expect(promoteAll()).toBeDisabled()

    let applied
    mockDataProvider.correctAlbum.mockReturnValue(
      new Promise((resolve) => {
        applied = resolve
      }),
    )
    fireEvent.click(applyButton())
    expect(promoteAll()).toBeDisabled()
    http.mockReturnValueOnce(reply(review('reloaded')))
    applied({ data: { done: true } })
    await waitFor(() => expect(promoteAll()).toBeEnabled())

    // Promote's plan open: Find waits (behind the dialog).
    http.mockReturnValueOnce(new Promise(() => {}))
    fireEvent.click(promoteAll())
    expect(
      screen.getByRole('button', {
        name: 'resources.album.musicbrainz.find',
        hidden: true,
      }),
    ).toBeDisabled()
  })
})
