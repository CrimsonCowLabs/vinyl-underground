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
  mockFit,
  mockEditing,
} = vi.hoisted(() => ({
  mockMatch: vi.fn(),
  mockFit: vi.fn(),
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
  fitMusicBrainzReleases: mockFit,
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

  describe('composer', () => {
    const songsWith = (...composers) =>
      composers.map((names, i) => ({
        id: `mf${i + 1}`,
        title: `T${i + 1}`,
        discNumber: 1,
        trackNumber: i + 1,
        participants: {
          composer: names.map((name) => ({ id: name, name })),
        },
      }))
    const showing = (songs) => {
      mockGetList.mockImplementation((resource) =>
        Promise.resolve({ data: resource === 'song' ? songs : [] }),
      )
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al1', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
    }

    it('shows the composers the Tracks share, and writes the new ones into every File', async () => {
      showing(songsWith(['Barry White', 'C Mack'], ['Barry White', 'C Mack']))
      await waitFor(() =>
        expect(field('album-composer').value).toBe('Barry White; C Mack'),
      )
      type('album-composer', 'Barry White;  Valerie Simpson ;')
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1]).toMatchObject({
        name: 'Recrod',
        composer: ['Barry White', 'Valerie Simpson'],
      })
    })

    it("says when the Tracks' composers differ, and keeps each Track's own unless some are typed", async () => {
      showing(songsWith(['B] White'], ['T] Lee', 'C Martin']))
      await screen.findByText('T1')
      expect(field('album-composer').value).toBe('')
      expect(
        screen.getByText('resources.album.editor.composerDiffers'),
      ).toBeTruthy()
      const button = screen.getByText('ra.action.save').closest('button')
      expect(button.disabled).toBe(true)

      type('album-composer', 'The Notorious B.I.G.')
      expect(button.disabled).toBe(false)
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1].composer).toEqual([
        'The Notorious B.I.G.',
      ])
    })

    it("doesn't send the composers when they are left as they were", async () => {
      showing(songsWith(['Barry White'], ['Barry White']))
      await waitFor(() =>
        expect(field('album-composer').value).toBe('Barry White'),
      )
      type('album-year', '1999')
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1]).not.toHaveProperty('composer')
    })
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
          filter: { album_id: 'al1', missing: false },
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
      expect(mockFindReleases).toHaveBeenCalledWith('Band', 'Record', false)
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

    describe('the search field', () => {
      const query = () => field('mbz-query')

      it("is filled in with the album's title, following the dialog's until typed in", () => {
        render(<EditAlbumButton record={record} />)
        open()
        expect(query().value).toBe('Recrod')
        type('album-name', 'Record')
        expect(query().value).toBe('Record')
        type('mbz-query', 'House Party')
        type('album-name', 'Other')
        expect(query().value).toBe('House Party')
        expect(
          screen.getByText('resources.album.musicbrainz.searchHint'),
        ).toBeTruthy()
      })

      it('searches what was typed, with the button or Enter, without saving the dialog', async () => {
        mockFindReleases.mockResolvedValue({ data: releases })
        render(<EditAlbumButton record={record} />)
        open()
        type('mbz-query', '7 68697 50272 5')
        fireEvent.keyDown(query(), { key: 'Enter', code: 'Enter' })
        await screen.findByTestId('mbz-release-rel-1')
        expect(mockFindReleases).toHaveBeenCalledWith(
          'Band',
          '7 68697 50272 5',
          false,
        )

        type('mbz-query', 'https://musicbrainz.org/release/rel-1')
        find()
        await waitFor(() =>
          expect(mockFindReleases).toHaveBeenLastCalledWith(
            'Band',
            'https://musicbrainz.org/release/rel-1',
            false,
          ),
        )
        expect(mockFindReleases).toHaveBeenCalledTimes(2)
        expect(mockCorrectAlbum).not.toHaveBeenCalled()
      })

      it('cannot search with nothing typed', () => {
        render(<EditAlbumButton record={record} />)
        open()
        type('mbz-query', '  ')
        expect(
          screen.getByText('resources.album.musicbrainz.find').closest('button')
            .disabled,
        ).toBe(true)
      })

      it('says a compilation is one, so the server searches it as Various Artists', async () => {
        mockFindReleases.mockResolvedValue({ data: [] })
        render(
          <EditAlbumButton
            record={{ ...record, albumArtist: 'DJ', compilation: true }}
          />,
        )
        open()
        find()
        await waitFor(() =>
          expect(mockFindReleases).toHaveBeenCalledWith('DJ', 'Recrod', true),
        )
      })

      it("says why a link to anything but a release can't be searched", async () => {
        mockFindReleases.mockRejectedValue({ body: { error: 'notRelease' } })
        render(<EditAlbumButton record={record} />)
        open()
        find()
        expect(
          await screen.findByText(
            'resources.album.musicbrainz.errors.notRelease',
          ),
        ).toBeTruthy()
      })

      it('pairs a release found by its barcode as any other', async () => {
        mockFindReleases.mockResolvedValue({ data: [releases[1]] })
        mockMatch.mockResolvedValue({ data: match })
        render(<EditAlbumButton record={record} />)
        open()
        type('mbz-query', '768697502725')
        find()
        fireEvent.click(await screen.findByTestId('mbz-release-rel-2'))
        await screen.findByTestId('mbz-pairing')
        expect(mockFindReleases).toHaveBeenCalledWith(
          'Band',
          '768697502725',
          false,
        )
        expect(mockMatch).toHaveBeenCalledWith('rel-2', 'al1')
        expect(screen.getByTestId('mbz-picked').textContent).toContain(
          'Vinyl, 2009)',
        )
      })
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
      genres: ['Alternative Rock', 'Rock'],
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
      expect(screen.queryByTestId('mbz-by-position')).toBeNull()
      // Only what differs is offered: no album artist, no track titles
      expect(screen.getByTestId('mbz-option-album')).toBeTruthy()
      expect(screen.getByTestId('mbz-option-date')).toBeTruthy()
      expect(screen.queryByTestId('mbz-option-albumArtist')).toBeNull()
      expect(screen.queryByTestId('mbz-option-titles')).toBeNull()
      expect(screen.getByTestId('mbz-option-replaceTrackIds')).toBeTruthy()
    })

    it("writes the picked release's IDs, cover and genres, unless the admin ticks its values or its track IDs", async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      expect(screen.getByTestId('mbz-option-cover').checked).toBe(true)
      expect(screen.getByTestId('mbz-option-genres').checked).toBe(true)
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
          artists: false,
        },
        replaceTrackIds: false,
        cover: true,
        genres: true,
      })
    })

    it("leaves the Files' genres alone when the admin unticks them", async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      fireEvent.click(screen.getByTestId('mbz-option-genres'))
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1]).toMatchObject({
        mbzAlbumId: 'rel-2',
        genres: false,
      })
    })

    it('says which Tracks pair by track number alone, and warns of nothing when every Track pairs', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({
        data: {
          ...match,
          holds: true,
          byPosition: 1,
          tracks: [
            match.tracks[0],
            {
              ...match.tracks[1],
              paired: { id: 'rt-2', title: 'Two', disc: 1, position: 2 },
              byPosition: true,
            },
          ],
          unpaired: [],
        },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      expect(screen.getByTestId('mbz-by-position').textContent).toContain(
        'resources.album.musicbrainz.pairing.byPosition',
      )
      expect(screen.getByTestId('mbz-pair-mf-2').textContent).toContain(
        'resources.album.musicbrainz.pairing.pairedByNumber',
      )
      expect(screen.getByTestId('mbz-pair-mf-1').textContent).not.toContain(
        'resources.album.musicbrainz.pairing.pairedByNumber',
      )
      expect(
        screen.queryByText(/resources.album.musicbrainz.pairing.doesntHold/),
      ).toBeNull()
    })

    it('says when the album is one disc of the release, pairing by number on it, and offers its track artists', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({
        data: {
          ...match,
          holds: true,
          byPosition: 2,
          disc: 2,
          discs: 2,
          canonical: { ...match.canonical, titles: 2, artists: 2 },
          tracks: match.tracks.map((t, i) => ({
            ...t,
            paired: {
              id: `rt-2-${i + 1}`,
              title: `Disc two ${i + 1}`,
              artist: 'Jaya',
              disc: 2,
              position: i + 1,
            },
            byPosition: true,
          })),
          unpaired: [],
        },
      })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      expect(screen.getByTestId('mbz-by-position').textContent).toContain(
        'resources.album.musicbrainz.pairing.byPositionDisc',
      )
      expect(screen.getByTestId('mbz-disc').textContent).toContain('"disc":2')
      expect(screen.getByTestId('mbz-pair-mf-1').textContent).toContain(
        '2-1. Disc two 1 (Jaya)',
      )
      const artists = screen.getByTestId('mbz-option-artists')
      expect(artists.checked).toBe(false)
      fireEvent.click(artists)
      fireEvent.click(screen.getByTestId('mbz-option-titles'))
      save()
      await waitFor(() => expect(mockCorrectAlbum).toHaveBeenCalled())
      expect(mockCorrectAlbum.mock.calls[0][1].canonical).toEqual({
        album: false,
        albumArtist: false,
        date: false,
        titles: true,
        artists: true,
      })
    })

    it('offers no genres when MusicBrainz has none for the album', async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: { ...match, genres: [] } })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      expect(screen.queryByTestId('mbz-option-genres')).toBeNull()
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

    it("says so when the album has no folder of its own for the release's cover", async () => {
      mockFindReleases.mockResolvedValue({ data: releases })
      mockMatch.mockResolvedValue({ data: match })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al3', done: true, pickCover: 'noFolder' },
      })
      render(<EditAlbumButton record={record} />)
      open()
      await pickRelease()
      save()

      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.musicbrainz.coverNoFolder',
          { type: 'warning' },
        ),
      )
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

    describe('the best match', () => {
      // The album has two Tracks; so have rel-a, rel-b and rel-c, rel-1 and rel-2 none known.
      const songs = [
        { id: 'mf1', title: 'One', discNumber: 1, trackNumber: 1 },
        { id: 'mf2', title: 'Two', discNumber: 1, trackNumber: 2 },
      ]
      const editions = [
        ...releases,
        ...['a', 'b', 'c'].map((x) => ({
          ...releases[0],
          id: `rel-${x}`,
          trackCount: 2,
        })),
        { ...releases[0], id: 'rel-long', trackCount: 3 },
      ]
      const fits = [
        {
          id: 'rel-a',
          paired: 2,
          trackCount: 2,
          albumTracks: 2,
          lengthDiff: 2.25,
          best: false,
        },
        {
          id: 'rel-b',
          paired: 2,
          trackCount: 2,
          albumTracks: 2,
          lengthDiff: 0.6,
          best: true,
        },
        {
          id: 'rel-c',
          paired: 1,
          trackCount: 2,
          albumTracks: 2,
          lengthDiff: null,
          best: false,
        },
      ]
      const pending = () => {
        let answer
        mockFit.mockReturnValueOnce(
          new Promise((resolve) => {
            answer = resolve
          }),
        )
        return (data) => answer({ data })
      }

      beforeEach(() => {
        mockGetList.mockImplementation((resource) =>
          Promise.resolve({ data: resource === 'song' ? songs : [] }),
        )
        // Each search answers anew, as the server does
        mockFindReleases.mockImplementation(() =>
          Promise.resolve({ data: [...editions] }),
        )
        mockFit.mockReset()
      })

      it('checks the releases with as many tracks as the album, marking the one that fits its Files best', async () => {
        const answer = pending()
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await screen.findByTestId('mbz-release-rel-b')
        await waitFor(() => expect(mockFit).toHaveBeenCalled())
        expect(mockFit).toHaveBeenCalledWith('al1', ['rel-a', 'rel-b', 'rel-c'])
        expect(screen.getByTestId('mbz-fitting').textContent).toContain(
          'resources.album.musicbrainz.fitting',
        )
        expect(screen.queryByTestId('mbz-best-match')).toBeNull()

        answer(fits)
        await waitFor(() =>
          expect(screen.queryByTestId('mbz-fitting')).toBeNull(),
        )
        const marks = screen.getAllByTestId('mbz-best-match')
        expect(marks).toHaveLength(1)
        expect(screen.getByTestId('mbz-release-rel-b').contains(marks[0])).toBe(
          true,
        )
        expect(marks[0].textContent).toBe(
          'resources.album.musicbrainz.bestMatch',
        )
        expect(screen.getByTestId('mbz-fit-rel-b').textContent).toBe(
          'resources.album.musicbrainz.fit.paired {"smart_count":2,"paired":2} · resources.album.musicbrainz.fit.lengths {"seconds":"0.6"}',
        )
        expect(screen.getByTestId('mbz-fit-rel-a').textContent).toContain(
          '{"seconds":"2.3"}',
        )
        // No length known: the pairing alone
        expect(screen.getByTestId('mbz-fit-rel-c').textContent).toBe(
          'resources.album.musicbrainz.fit.paired {"smart_count":2,"paired":1}',
        )
        expect(screen.queryByTestId('mbz-fit-rel-1')).toBeNull()
        expect(screen.queryByTestId('mbz-fit-rel-long')).toBeNull()
      })

      it('leaves out a quarantined Track, so it neither shows among the track numbers nor counts as one of the tracks', async () => {
        // A quarantined Track stays a missing row; the server leaves missing ones out when asked
        const quarantined = {
          id: 'mf3',
          title: 'Two',
          discNumber: 1,
          trackNumber: 2,
          missing: true,
          quarantined: true,
        }
        mockGetList.mockImplementation((resource, { filter }) =>
          Promise.resolve({
            data:
              resource === 'song'
                ? [...songs, quarantined].filter(
                    (s) => filter.missing !== false || !s.missing,
                  )
                : [],
          }),
        )
        mockFit.mockResolvedValue({ data: fits })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByTestId('track-number-mf1')
        expect(screen.getByTestId('track-number-mf2')).toBeTruthy()
        expect(screen.queryByTestId('track-number-mf3')).toBeNull()
        // No clash with the Track of the same number in the Library
        expect(screen.queryByRole('alert')).toBeNull()

        find()
        await waitFor(() => expect(mockFit).toHaveBeenCalled())
        expect(mockFit).toHaveBeenCalledWith('al1', ['rel-a', 'rel-b', 'rel-c'])
      })

      it('checks a release with a disc of as many tracks as the album', async () => {
        mockFindReleases.mockResolvedValue({
          data: [
            {
              ...releases[0],
              id: 'rel-double',
              trackCount: 5,
              discTrackCounts: [3, 2],
            },
            {
              ...releases[0],
              id: 'rel-other',
              trackCount: 5,
              discTrackCounts: [4, 1],
            },
          ],
        })
        mockFit.mockResolvedValue({ data: [] })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await waitFor(() =>
          expect(mockFit).toHaveBeenCalledWith('al1', ['rel-double']),
        )
      })

      it('checks five releases at most, and none with no track count like the album', async () => {
        mockFindReleases.mockResolvedValue({
          data: ['a', 'b', 'c', 'd', 'e', 'f'].map((x) => ({
            ...releases[0],
            id: `rel-${x}`,
            trackCount: 2,
          })),
        })
        mockFit.mockResolvedValue({ data: [] })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await waitFor(() =>
          expect(mockFit).toHaveBeenCalledWith('al1', [
            'rel-a',
            'rel-b',
            'rel-c',
            'rel-d',
            'rel-e',
          ]),
        )

        mockFit.mockClear()
        mockFindReleases.mockResolvedValue({ data: releases })
        find()
        await screen.findByTestId('mbz-release-rel-2')
        await waitFor(() =>
          expect(screen.queryByTestId('mbz-fitting')).toBeNull(),
        )
        expect(mockFit).not.toHaveBeenCalled()
      })

      it('lets a release be picked while the check runs', async () => {
        pending()
        mockMatch.mockResolvedValue({ data: match })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        fireEvent.click(await screen.findByTestId('mbz-release-rel-a'))
        expect(mockMatch).toHaveBeenCalledWith('rel-a', 'al1')
        expect(screen.getByTestId('mbz-fitting')).toBeTruthy()
      })

      it('drops an answer for a search run again since', async () => {
        const stale = pending()
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await waitFor(() => expect(mockFit).toHaveBeenCalledTimes(1))

        // The second search finds the same releases; its own check is still running
        const fresh = pending()
        find()
        await waitFor(() => expect(mockFit).toHaveBeenCalledTimes(2))
        stale(fits)
        await new Promise((r) => setTimeout(r, 0))
        expect(screen.queryByTestId('mbz-best-match')).toBeNull()
        expect(screen.queryByTestId('mbz-fit-rel-b')).toBeNull()
        expect(screen.getByTestId('mbz-fitting')).toBeTruthy()

        fresh([{ ...fits[0], best: true }])
        await screen.findByTestId('mbz-best-match')
        expect(
          screen
            .getByTestId('mbz-release-rel-a')
            .contains(screen.getByTestId('mbz-best-match')),
        ).toBe(true)
      })

      it('lists the releases as they are when the check fails', async () => {
        mockFit.mockRejectedValue({ body: { error: 'busy' } })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await waitFor(() => expect(mockFit).toHaveBeenCalled())
        await waitFor(() =>
          expect(screen.queryByTestId('mbz-fitting')).toBeNull(),
        )
        expect(screen.getByTestId('mbz-release-rel-b')).toBeTruthy()
        expect(screen.queryByTestId('mbz-best-match')).toBeNull()
        expect(screen.queryByText(/errors.busy/)).toBeNull()
        // Only a line under the list says why none is marked
        expect(screen.getByTestId('mbz-no-best-match').textContent).toBe(
          'resources.album.musicbrainz.noBestMatch.busy',
        )
      })

      it("says the check failed when MusicBrainz couldn't be reached for it", async () => {
        mockFit.mockRejectedValue({ body: { error: 'unavailable' } })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        expect(
          (await screen.findByTestId('mbz-no-best-match')).textContent,
        ).toBe('resources.album.musicbrainz.noBestMatch.failed')
      })

      it("says why none is marked when no release has the album's number of Tracks", async () => {
        // In Utero: 13 Tracks, a hidden track kept as its own File; every edition has 12
        mockFindReleases.mockResolvedValue({ data: releases })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await screen.findByTestId('mbz-release-rel-2')
        expect(mockFit).not.toHaveBeenCalled()
        expect(screen.getByTestId('mbz-no-best-match').textContent).toBe(
          'resources.album.musicbrainz.noBestMatch.noCandidates {"smart_count":2}',
        )
      })

      it('says why none is marked when no release checked pairs most of the Tracks, once checked', async () => {
        const answer = pending()
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await waitFor(() => expect(mockFit).toHaveBeenCalled())
        expect(screen.getByTestId('mbz-fitting')).toBeTruthy()
        expect(screen.queryByTestId('mbz-no-best-match')).toBeNull()

        answer(fits.map((f) => ({ ...f, paired: 1, best: false })))
        expect(
          (await screen.findByTestId('mbz-no-best-match')).textContent,
        ).toBe(
          'resources.album.musicbrainz.noBestMatch.noGoodFit {"smart_count":2}',
        )
        expect(screen.queryByTestId('mbz-best-match')).toBeNull()
      })

      it('says nothing more when a release is marked, or when nothing is found', async () => {
        mockFit.mockResolvedValue({ data: fits })
        render(<EditAlbumButton record={record} />)
        open()
        await screen.findByText('One')
        find()
        await screen.findByTestId('mbz-best-match')
        expect(screen.queryByTestId('mbz-no-best-match')).toBeNull()

        mockFindReleases.mockResolvedValue({ data: [] })
        find()
        await screen.findByText('resources.album.musicbrainz.noResults')
        expect(screen.queryByTestId('mbz-no-best-match')).toBeNull()
      })
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
