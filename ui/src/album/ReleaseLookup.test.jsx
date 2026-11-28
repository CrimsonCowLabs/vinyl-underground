import React, { useState } from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ReleaseLookup } from './ReleaseLookup'
import { useReleaseLookup } from './useReleaseLookup'

const { mockCorrectAlbum, mockFindReleases, mockMatch, mockFit } = vi.hoisted(
  () => ({
    mockCorrectAlbum: vi.fn(),
    mockFindReleases: vi.fn(),
    mockMatch: vi.fn(),
    mockFit: vi.fn(),
  }),
)
// The dataProvider is the same object across renders, as react-admin's is
const mockDataProvider = {
  correctAlbum: mockCorrectAlbum,
  findMusicBrainzReleases: mockFindReleases,
  matchMusicBrainzRelease: mockMatch,
  fitMusicBrainzReleases: mockFit,
}

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useDataProvider: () => mockDataProvider,
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

const album = {
  id: 'al1',
  trackCount: 2,
  artist: 'Band',
  title: 'Record',
  compilation: false,
}

const releases = [
  {
    id: 'rel-1',
    releaseGroupId: 'rg-1',
    title: 'Record',
    artist: 'Band',
    year: '1998',
    description: 'Record (GB, CD, 1998)',
  },
]

const match = {
  release: { id: 'rel-1', albumArtist: 'Band' },
  holds: true,
  tracks: [
    {
      id: 'mf-1',
      title: 'One',
      disc: 1,
      track: 1,
      paired: { id: 'rt-1', title: 'One', disc: 1, position: 1 },
    },
    { id: 'mf-2', title: 'Bonus', disc: 1, track: 9 },
  ],
  unpaired: [],
  albumArtistMatches: true,
  canonical: {
    album: { from: 'Recrod', to: 'Record' },
    albumArtist: {},
    date: {},
    titles: 0,
  },
  genres: ['Rock'],
}

// A caller of the release flow, as the album Edit dialog and the review card are: it shows the
// lookup, sends its own fields with the apply, and shows what came of it.
const Caller = ({ fields = { name: 'Record' } }) => {
  const lookup = useReleaseLookup(album)
  const [outcome, setOutcome] = useState(null)
  return (
    <>
      <ReleaseLookup lookup={lookup} />
      <span data-testid="settled">{String(lookup.settled)}</span>
      <span data-testid="paired">{String(lookup.paired)}</span>
      <button
        onClick={() =>
          lookup
            .apply(fields)
            .then(setOutcome)
            .catch((err) => setOutcome({ failed: err.body.error }))
        }
      >
        apply
      </button>
      {outcome && <pre data-testid="outcome">{JSON.stringify(outcome)}</pre>}
    </>
  )
}

const pick = async () => {
  fireEvent.click(screen.getByText('resources.album.musicbrainz.find'))
  fireEvent.click(await screen.findByTestId('mbz-release-rel-1'))
}
const outcome = async () =>
  JSON.parse((await screen.findByTestId('outcome')).textContent)

describe('ReleaseLookup', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFindReleases.mockResolvedValue({ data: releases })
    mockFit.mockResolvedValue({ data: [] })
    mockMatch.mockResolvedValue({ data: match })
    mockCorrectAlbum.mockResolvedValue({ data: { done: true } })
  })

  it("searches by the album's artist and title, and pairs the picked release with its Tracks", async () => {
    render(<Caller />)
    expect(screen.getByTestId('settled').textContent).toBe('true')
    await pick()
    expect(mockFindReleases).toHaveBeenCalledWith('Band', 'Record', false)
    expect(mockMatch).toHaveBeenCalledWith('rel-1', 'al1')
    await screen.findByTestId('mbz-pairing')
    expect(screen.getByTestId('mbz-pair-mf-2').textContent).toContain(
      'resources.album.musicbrainz.pairing.unpaired',
    )
    expect(screen.getByTestId('settled').textContent).toBe('true')
    expect(screen.getByTestId('paired').textContent).toBe('true')
  })

  describe('apply', () => {
    const apply = () => fireEvent.click(screen.getByText('apply'))

    it("sends only the caller's fields when no release is picked", async () => {
      render(<Caller fields={{ name: 'Record', year: 1998 }} />)
      apply()
      await outcome()
      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
        name: 'Record',
        year: 1998,
      })
    })

    it("sends the paired release with what the admin ticked, cover and genres to start with, and the caller's fields", async () => {
      render(<Caller fields={{ name: 'Record', year: 1998 }} />)
      await pick()
      await screen.findByTestId('mbz-pairing')
      fireEvent.click(screen.getByTestId('mbz-option-album'))
      fireEvent.click(screen.getByTestId('mbz-option-genres'))
      apply()
      await outcome()
      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
        name: 'Record',
        year: 1998,
        mbzAlbumId: 'rel-1',
        canonical: {
          album: true,
          albumArtist: false,
          date: false,
          titles: false,
          artists: false,
        },
        replaceTrackIds: false,
        cover: true,
        genres: false,
      })
    })

    it("leaves out a release whose pairing couldn't be had, saying why", async () => {
      mockMatch.mockRejectedValue({ body: { error: 'busy' } })
      render(<Caller />)
      await pick()
      expect(
        await screen.findByText('resources.album.musicbrainz.errors.busy'),
      ).toBeTruthy()
      expect(screen.getByTestId('settled').textContent).toBe('true')
      expect(screen.getByTestId('paired').textContent).toBe('false')
      apply()
      await outcome()
      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', { name: 'Record' })
    })

    it("drops the first release's pairing, error and options at once when another is picked, so the apply carries only the second's", async () => {
      const second = {
        ...releases[0],
        id: 'rel-2',
        description: 'Record (US, CD, 1999)',
      }
      mockFindReleases.mockResolvedValue({ data: [...releases, second] })
      mockMatch.mockImplementation((id) =>
        Promise.resolve({
          data: { ...match, release: { ...match.release, id } },
        }),
      )
      // Every state the flow is seen in: a picked release is never shown, nor applied, with
      // another release's pairing or options.
      const seen = []
      const Probe = () => {
        const lookup = useReleaseLookup(album)
        seen.push({
          release: lookup.release?.id,
          match: lookup.match?.release.id,
          album: lookup.options.canonical.album,
        })
        return (
          <>
            <ReleaseLookup lookup={lookup} />
            <button
              onClick={() => lookup.apply({ name: 'Record' }).then(() => {})}
            >
              apply
            </button>
          </>
        )
      }
      render(<Probe />)
      await pick()
      await screen.findByTestId('mbz-pairing')
      fireEvent.click(screen.getByTestId('mbz-option-album'))
      fireEvent.click(screen.getByTestId('mbz-release-rel-2'))
      await waitFor(() =>
        expect(mockMatch).toHaveBeenCalledWith('rel-2', 'al1'),
      )
      await screen.findByTestId('mbz-pairing')
      fireEvent.click(screen.getByTestId('mbz-option-genres'))
      apply()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())

      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
        name: 'Record',
        mbzAlbumId: 'rel-2',
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
      const withSecond = seen.filter((s) => s.release === 'rel-2')
      expect(withSecond.length).toBeGreaterThan(0)
      for (const s of withSecond) {
        expect(s.match === undefined || s.match === 'rel-2').toBe(true)
        expect(s.album).toBe(false)
      }
    })

    it('passes on a refused apply for the caller to say why', async () => {
      mockCorrectAlbum.mockRejectedValue({ body: { error: 'sharedFolder' } })
      render(<Caller />)
      apply()
      expect(await outcome()).toEqual({ failed: 'sharedFolder' })
    })
  })

  describe('what came of an apply', () => {
    const applied = async (data) => {
      mockCorrectAlbum.mockResolvedValue({ data })
      render(<Caller />)
      fireEvent.click(screen.getByText('apply'))
      return outcome()
    }

    it('is nothing worth a word over the usual one when it all went in', async () => {
      expect(await applied({ done: true, albumId: 'al2' })).toEqual({
        done: true,
        albumId: 'al2',
        note: null,
        notifyOptions: { type: 'info' },
      })
    })

    it('is still running when the job finishes in the background', async () => {
      expect(await applied({ done: false })).toEqual({
        done: false,
        note: null,
        notifyOptions: { type: 'info' },
      })
    })

    it("says a release that couldn't reach every File was rolled back, before any Files skipped", async () => {
      expect(
        await applied({ done: true, rolledBack: true, job: { skipped: 2 } }),
      ).toEqual({
        done: true,
        note: 'resources.album.musicbrainz.rolledBack',
        notifyOptions: { type: 'warning', messageArgs: { smart_count: 2 } },
      })
    })

    it("counts the Files the job couldn't write", async () => {
      expect(
        await applied({ done: true, job: { skipped: 3 }, pickCover: 'none' }),
      ).toEqual({
        done: true,
        note: 'resources.album.editor.skipped',
        notifyOptions: { type: 'warning', messageArgs: { smart_count: 3 } },
      })
    })

    it.each([
      ['none', 'resources.album.musicbrainz.noCover'],
      ['failed', 'resources.album.musicbrainz.coverFailed'],
      ['disabled', 'resources.album.musicbrainz.coverDisabled'],
      ['noFolder', 'resources.album.musicbrainz.coverNoFolder'],
    ])('says why no cover was written (%s)', async (pickCover, note) => {
      expect(await applied({ done: true, pickCover })).toEqual({
        done: true,
        note,
        notifyOptions: { type: 'warning' },
      })
    })
  })
})
