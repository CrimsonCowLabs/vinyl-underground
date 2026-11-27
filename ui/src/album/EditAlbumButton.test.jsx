import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditAlbumButton } from './EditAlbumButton'

const {
  mockPermissions,
  mockCorrectAlbum,
  mockCorrectTrackNumbers,
  mockGetList,
  mockNotify,
  mockRedirect,
  mockRefresh,
  mockFindReleases,
  mockMatch,
  mockEditing,
} = vi.hoisted(() => ({
  mockMatch: vi.fn(),
  mockEditing: { writable: true },
  mockPermissions: { value: 'admin' },
  mockCorrectAlbum: vi.fn(),
  mockCorrectTrackNumbers: vi.fn(),
  mockGetList: vi.fn(),
  mockNotify: vi.fn(),
  mockRedirect: vi.fn(),
  mockRefresh: vi.fn(),
  mockFindReleases: vi.fn(),
}))
// The dataProvider is the same object across renders, as react-admin's is
const mockDataProvider = {
  correctAlbum: mockCorrectAlbum,
  correctTrackNumbers: mockCorrectTrackNumbers,
  getList: mockGetList,
  findMusicBrainzReleases: mockFindReleases,
  matchMusicBrainzRelease: mockMatch,
}

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    Button: ({ children, onClick, label }) => (
      <button onClick={onClick}>
        {label}
        {children}
      </button>
    ),
    useDataProvider: () => mockDataProvider,
    useNotify: () => mockNotify,
    useRedirect: () => mockRedirect,
    useRefresh: () => mockRefresh,
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => ({
    loaded: true,
    enabled: mockEditing.writable,
    isWritable: (id) => (id === 1 ? mockEditing.writable : undefined),
  }),
}))

const record = {
  id: 'al1',
  libraryId: 1,
  name: 'Recrod',
  albumArtist: 'Band',
  maxYear: 1998,
}

const open = () =>
  fireEvent.click(screen.getByText('resources.album.actions.edit'))
const field = (id) => screen.getByTestId(id)
const type = (id, value) => fireEvent.change(field(id), { target: { value } })
const save = () => fireEvent.click(screen.getByText('ra.action.save'))

describe('EditAlbumButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPermissions.value = 'admin'
    mockEditing.writable = true
    mockGetList.mockResolvedValue({ data: [] })
  })

  it('is not offered to regular users', () => {
    mockPermissions.value = 'user'
    render(<EditAlbumButton record={record} />)
    expect(screen.queryByText('resources.album.actions.edit')).toBeNull()
  })

  it("is not offered while the album's library is read-only", () => {
    mockEditing.writable = false
    render(<EditAlbumButton record={record} />)
    expect(screen.queryByText('resources.album.actions.edit')).toBeNull()
  })

  it('opens with the title, album artist and year filled in', () => {
    render(<EditAlbumButton record={{ ...record, maxOriginalYear: 1969 }} />)
    open()
    expect(field('album-name').value).toBe('Recrod')
    expect(field('album-artist').value).toBe('Band')
    expect(field('album-year').value).toBe('1969')
  })

  it('writes the album to its Files and opens it, under its new ID, once the job is done', async () => {
    mockCorrectAlbum.mockResolvedValue({
      data: { albumId: 'al2', done: true, job: { skipped: 0 } },
    })
    render(<EditAlbumButton record={record} />)
    open()
    type('album-name', ' Record ')
    type('album-year', '1999')
    save()

    await waitFor(() =>
      expect(mockRedirect).toHaveBeenCalledWith('/album/al2/show'),
    )
    expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
      name: 'Record',
      albumArtist: 'Band',
      year: 1999,
    })
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.album.editor.corrected',
      expect.anything(),
    )
    expect(screen.queryByTestId('album-name')).toBeNull()
  })

  it('says how many Files the job skipped', async () => {
    mockCorrectAlbum.mockResolvedValue({
      data: { albumId: 'al2', done: true, job: { skipped: 2 } },
    })
    render(<EditAlbumButton record={record} />)
    open()
    type('album-name', 'Record')
    save()

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.album.editor.skipped',
        { type: 'warning', messageArgs: { smart_count: 2 } },
      ),
    )
    expect(mockRedirect).toHaveBeenCalledWith('/album/al2/show')
  })

  it('closes once saved, back to the albums, when the job carries on in the background', async () => {
    mockCorrectAlbum.mockResolvedValue({
      data: { jobId: 'job-1', background: true, done: false },
    })
    render(<EditAlbumButton record={record} />)
    open()
    type('album-name', 'Record')
    save()

    await waitFor(() => expect(mockRedirect).toHaveBeenCalledWith('/album'))
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.album.editor.correcting',
      expect.anything(),
    )
  })

  it('offers existing Artists as the album artist as you type', async () => {
    mockGetList.mockImplementation((resource) =>
      Promise.resolve({
        data: resource === 'artist' ? [{ id: 'ar2', name: 'The Band' }] : [],
      }),
    )
    render(<EditAlbumButton record={record} />)
    open()
    fireEvent.change(field('album-artist'), { target: { value: 'The B' } })

    await waitFor(() =>
      expect(mockGetList).toHaveBeenCalledWith(
        'artist',
        expect.objectContaining({ filter: { name: 'The B' } }),
      ),
    )
    fireEvent.click(await screen.findByText('The Band'))
    expect(field('album-artist').value).toBe('The Band')
  })

  it('cannot save an unchanged album, an empty title or a year that is no year', () => {
    render(<EditAlbumButton record={record} />)
    open()
    const button = screen.getByText('ra.action.save').closest('button')
    expect(button.disabled).toBe(true)
    type('album-name', '')
    expect(button.disabled).toBe(true)
    type('album-name', 'Record')
    expect(button.disabled).toBe(false)
    type('album-year', '99')
    expect(button.disabled).toBe(true)
  })

  it('shows why the server refused the Correction', async () => {
    mockCorrectAlbum.mockRejectedValue({ body: { error: 'sharedFolder' } })
    render(<EditAlbumButton record={record} />)
    open()
    type('album-name', 'Record')
    save()

    expect(
      await screen.findByText(/resources.album.editor.errors.sharedFolder/),
    ).toBeTruthy()
    expect(mockRedirect).not.toHaveBeenCalled()
  })

  describe('track numbers', () => {
    const songs = [
      { id: 'mf1', title: 'One', discNumber: 1, trackNumber: 2 },
      { id: 'mf2', title: 'Two', discNumber: 1, trackNumber: 1 },
    ]
    const number = (id, value) =>
      fireEvent.change(screen.getByTestId(`track-number-${id}`), {
        target: { value },
      })

    beforeEach(() => {
      mockGetList.mockImplementation((resource) =>
        Promise.resolve({ data: resource === 'song' ? songs : [] }),
      )
    })

    it("lists the album's Tracks in album order", async () => {
      render(<EditAlbumButton record={record} />)
      open()
      expect(await screen.findByText('One')).toBeTruthy()
      expect(mockGetList).toHaveBeenCalledWith(
        'song',
        expect.objectContaining({
          filter: { album_id: 'al1' },
          sort: { field: 'album', order: 'ASC' },
        }),
      )
    })

    it('saves the changed numbers together, and stays on the album, whose ID they keep', async () => {
      mockCorrectTrackNumbers.mockResolvedValue({
        data: { albumId: 'al1', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await screen.findByText('One')
      number('mf1', '1')
      number('mf2', '2')
      save()

      await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
      expect(mockCorrectTrackNumbers).toHaveBeenCalledWith('al1', [
        { id: 'mf1', discNumber: 1, trackNumber: 1 },
        { id: 'mf2', discNumber: 1, trackNumber: 2 },
      ])
      expect(mockCorrectAlbum).not.toHaveBeenCalled()
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.album.editor.corrected',
        expect.anything(),
      )
    })

    it('saves the track numbers with the album, as one job, and opens the album', async () => {
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al2', done: true, job: { skipped: 0 } },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await screen.findByText('One')
      number('mf1', '3')
      type('album-name', 'Record')
      save()

      await waitFor(() =>
        expect(mockRedirect).toHaveBeenCalledWith('/album/al2/show'),
      )
      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
        name: 'Record',
        albumArtist: 'Band',
        year: 1998,
        tracks: [{ id: 'mf1', discNumber: 1, trackNumber: 3 }],
      })
      expect(mockCorrectTrackNumbers).not.toHaveBeenCalled()
    })

    it('shows why a refused save was refused, and saves it all again on retry', async () => {
      mockCorrectAlbum
        .mockRejectedValueOnce({ body: { error: 'changedOnDisk' } })
        .mockResolvedValueOnce({ data: { albumId: 'al2', done: true } })
      render(<EditAlbumButton record={record} />)
      open()
      await screen.findByText('One')
      number('mf1', '3')
      type('album-name', 'Record')
      save()
      expect(
        await screen.findByText(/resources.album.editor.errors.changedOnDisk/),
      ).toBeTruthy()

      save()
      await waitFor(() =>
        expect(mockRedirect).toHaveBeenCalledWith('/album/al2/show'),
      )
      expect(mockCorrectAlbum).toHaveBeenCalledTimes(2)
      expect(mockCorrectAlbum.mock.calls[1][1].tracks).toEqual([
        { id: 'mf1', discNumber: 1, trackNumber: 3 },
      ])
    })

    it('warns about a duplicate number but still saves; a number that is no number blocks saving', async () => {
      render(<EditAlbumButton record={record} />)
      open()
      await screen.findByText('One')
      const button = screen.getByText('ra.action.save').closest('button')
      number('mf1', '1')
      expect(screen.getByRole('alert').textContent).toContain(
        'resources.album.editor.duplicateNumber',
      )
      expect(button.disabled).toBe(false)
      number('mf1', 'x')
      expect(button.disabled).toBe(true)
    })
  })

  describe('Find on MusicBrainz', () => {
    const releases = [
      {
        id: 'rel-1',
        releaseGroupId: 'rg-1',
        title: 'Record',
        artist: 'Band',
        year: '1998',
        country: 'GB',
        format: '2×CD',
        status: 'Official',
        description: 'Record (GB, 2×CD, 1998)',
        thumbnailUrl: 'https://coverartarchive.org/release/rel-1/front-250',
      },
      {
        id: 'rel-2',
        releaseGroupId: 'rg-1',
        title: 'Record',
        artist: 'Band',
        year: '2009',
        country: 'US',
        format: '12" Vinyl',
        description: 'Record (US, 12" Vinyl, 2009)',
        thumbnailUrl: 'https://coverartarchive.org/release/rel-2/front-250',
      },
    ]
    const find = () =>
      fireEvent.click(screen.getByText('resources.album.musicbrainz.find'))

    it('searches by the album artist and title in the dialog, listing what tells the releases apart', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      render(<EditAlbumButton record={record} />)
      open()
      type('album-name', 'Record')
      find()

      const first = await screen.findByTestId('mbz-release-rel-1')
      expect(mockFindReleases).toHaveBeenCalledWith('Band', 'Record')
      expect(first.textContent).toContain('Record – Band')
      expect(first.textContent).toContain('1998 · GB · 2×CD · Official')
      const img = first.querySelector('img')
      expect(img.getAttribute('src')).toBe(
        'https://coverartarchive.org/release/rel-1/front-250',
      )
      expect(img.getAttribute('src')).not.toContain('jwt')
      expect(screen.getByTestId('mbz-release-rel-2').textContent).toContain(
        '2009 · US · 12" Vinyl',
      )
    })

    it('shows a disc for a release with no cover in the archive', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      render(<EditAlbumButton record={record} />)
      open()
      find()
      const first = await screen.findByTestId('mbz-release-rel-1')
      fireEvent.error(first.querySelector('img'))
      expect(
        first.querySelector('[data-testid="mbz-no-thumbnail"]'),
      ).toBeTruthy()
    })

    // How the album's two Tracks pair with rel-2: One by title, though its File names another
    // recording; Bonus with no track of the release.
    const match = {
      release: { id: 'rel-2', albumArtist: 'Band' },
      holds: false,
      tracks: [
        {
          id: 'mf-1',
          title: 'One',
          disc: 1,
          track: 1,
          paired: { id: 'rt-1', title: 'One', disc: 1, position: 1 },
          mismatch: true,
        },
        { id: 'mf-2', title: 'Bonus', disc: 1, track: 9 },
      ],
      unpaired: [{ id: 'rt-2', title: 'Two' }],
      albumArtistMatches: true,
      canonical: {
        album: { from: 'Recrod', to: 'Record' },
        albumArtist: {},
        date: { from: '1998', to: '2009-04-01' },
        titles: 0,
      },
    }
    const pickRelease = async () => {
      find()
      fireEvent.click(await screen.findByTestId('mbz-release-rel-2'))
      await screen.findByTestId('mbz-pairing')
    }

    it('shows how the Tracks pair with the picked release before saving, with those left unpaired and the IDs that differ', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      let answer
      mockMatch.mockReturnValue(
        new Promise((resolve) => {
          answer = resolve
        }),
      )
      render(<EditAlbumButton record={record} />)
      open()
      const button = screen.getByText('ra.action.save').closest('button')
      find()
      fireEvent.click(await screen.findByTestId('mbz-release-rel-2'))
      expect(mockMatch).toHaveBeenCalledWith('rel-2', 'al1')
      // Not saved before the pairing is shown
      expect(screen.getByTestId('mbz-pairing-loading')).toBeTruthy()
      expect(button.disabled).toBe(true)
      answer({ data: match })

      await screen.findByTestId('mbz-pairing')
      expect(button.disabled).toBe(false)
      expect(screen.getByTestId('mbz-pair-mf-1').textContent).toContain(
        'resources.album.musicbrainz.pairing.mismatch',
      )
      expect(screen.getByTestId('mbz-pair-mf-2').textContent).toContain(
        'resources.album.musicbrainz.pairing.unpaired',
      )
      expect(screen.getByTestId('mbz-unpaired').textContent).toContain(
        '"smart_count":1',
      )
      expect(
        screen.getByText(/resources.album.musicbrainz.pairing.doesntHold/),
      ).toBeTruthy()
      // Only what differs is offered: no album artist, no track titles
      expect(screen.getByTestId('mbz-option-album')).toBeTruthy()
      expect(screen.getByTestId('mbz-option-date')).toBeTruthy()
      expect(screen.queryByTestId('mbz-option-albumArtist')).toBeNull()
      expect(screen.queryByTestId('mbz-option-titles')).toBeNull()
      expect(screen.getByTestId('mbz-option-replaceTrackIds')).toBeTruthy()
    })

    it("writes the picked release's IDs and cover, unless the admin ticks its values or its track IDs", async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      expect(screen.getByTestId('mbz-option-cover').checked).toBe(true)
      save()
      await waitFor(() =>
        expect(mockRedirect).toHaveBeenCalledWith('/album/al3/show'),
      )
      expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
        name: 'Recrod',
        albumArtist: 'Band',
        year: 1998,
        mbzAlbumId: 'rel-2',
        canonical: {
          album: false,
          albumArtist: false,
          date: false,
          titles: false,
        },
        replaceTrackIds: false,
        cover: true,
      })
    })

    it("leaves the album's cover alone when the admin unticks the release's cover", async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      fireEvent.click(screen.getByTestId('mbz-option-cover'))
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1]).toMatchObject({
        mbzAlbumId: 'rel-2',
        cover: false,
      })
    })

    it('sends what the admin ticks, and locks the fields the release fills', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      fireEvent.click(screen.getByTestId('mbz-option-album'))
      fireEvent.click(screen.getByTestId('mbz-option-replaceTrackIds'))
      expect(field('album-name').disabled).toBe(true)
      expect(field('album-year').disabled).toBe(false)
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1]).toMatchObject({
        mbzAlbumId: 'rel-2',
        canonical: { album: true, date: false },
        replaceTrackIds: true,
        cover: true,
      })
    })

    it('says nothing was saved when a File could not be written and the others were put back', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { done: true, rolledBack: true, job: { skipped: 1 } },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      save()
      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.musicbrainz.rolledBack',
          { type: 'warning', messageArgs: { smart_count: 1 } },
        ),
      )
      expect(mockRefresh).toHaveBeenCalled()
    })

    it('shows why the pairing could not be had, and saves the rest without the release', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockRejectedValue({ body: { error: 'busy' } })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al1', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      find()
      fireEvent.click(await screen.findByTestId('mbz-release-rel-2'))
      expect(
        await screen.findByText('resources.album.musicbrainz.errors.busy'),
      ).toBeTruthy()
      const button = screen.getByText('ra.action.save').closest('button')
      expect(button.disabled).toBe(true)
      type('album-year', '1999')
      expect(button.disabled).toBe(false)
      save()
      await waitFor(() =>
        expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
          name: 'Recrod',
          albumArtist: 'Band',
          year: 1999,
        }),
      )
    })

    it('says so when the picked release brought no cover', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true, pickCover: 'failed' },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      save()

      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.musicbrainz.coverFailed',
          { type: 'warning' },
        ),
      )
      expect(mockRedirect).toHaveBeenCalledWith('/album/al3/show')
    })

    it('lets a picked release be dropped before saving', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      render(<EditAlbumButton record={record} />)
      open()
      find()
      fireEvent.click(await screen.findByTestId('mbz-release-rel-1'))
      fireEvent.click(screen.getByText('resources.album.musicbrainz.clear'))
      expect(screen.queryByTestId('mbz-picked')).toBeNull()
      expect(
        screen.getByText('ra.action.save').closest('button').disabled,
      ).toBe(true)
    })

    it('shows why when MusicBrainz is down, and the other edits can still be saved', async () => {
      mockFindReleases.mockRejectedValue({ body: { error: 'unavailable' } })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al2', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      find()
      expect(
        await screen.findByText(
          'resources.album.musicbrainz.errors.unavailable',
        ),
      ).toBeTruthy()

      type('album-name', 'Record')
      save()
      await waitFor(() =>
        expect(mockCorrectAlbum).toHaveBeenCalledWith('al1', {
          name: 'Record',
          albumArtist: 'Band',
          year: 1998,
        }),
      )
    })

    it('says so when nothing matches', async () => {
      mockFindReleases.mockResolvedValue({ data: [] })
      render(<EditAlbumButton record={record} />)
      open()
      find()
      expect(
        await screen.findByText('resources.album.musicbrainz.noResults'),
      ).toBeTruthy()
    })
  })
})
