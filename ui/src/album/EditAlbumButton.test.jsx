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
} = vi.hoisted(() => ({
  mockPermissions: { value: 'admin' },
  mockCorrectAlbum: vi.fn(),
  mockCorrectTrackNumbers: vi.fn(),
  mockGetList: vi.fn(),
  mockNotify: vi.fn(),
  mockRedirect: vi.fn(),
  mockRefresh: vi.fn(),
}))
// The dataProvider is the same object across renders, as react-admin's is
const mockDataProvider = {
  correctAlbum: mockCorrectAlbum,
  correctTrackNumbers: mockCorrectTrackNumbers,
  getList: mockGetList,
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

const record = {
  id: 'al1',
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
    mockGetList.mockResolvedValue({ data: [] })
  })

  it('is not offered to regular users', () => {
    mockPermissions.value = 'user'
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

  it('saves the Correction and opens the corrected album once the rescan is done', async () => {
    mockCorrectAlbum.mockResolvedValue({
      data: { albumId: 'al2', done: true },
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

  it('closes once saved, back to the albums, when the rescan carries on in the background', async () => {
    mockCorrectAlbum.mockResolvedValue({ data: { done: false } })
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

    it('saves the track numbers, then the album, and opens the corrected album', async () => {
      mockCorrectTrackNumbers.mockResolvedValue({
        data: { albumId: 'al1', done: true },
      })
      mockCorrectAlbum.mockResolvedValue({
        data: { albumId: 'al2', done: true },
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
      expect(mockCorrectTrackNumbers).toHaveBeenCalledWith('al1', [
        { id: 'mf1', discNumber: 1, trackNumber: 3 },
      ])
      expect(mockCorrectTrackNumbers.mock.invocationCallOrder[0]).toBeLessThan(
        mockCorrectAlbum.mock.invocationCallOrder[0],
      )
    })

    it('retries only the album when the track numbers were saved but the album was refused', async () => {
      mockCorrectTrackNumbers.mockResolvedValue({
        data: { albumId: 'al1', done: true },
      })
      mockCorrectAlbum
        .mockRejectedValueOnce({ body: { error: 'internal' } })
        .mockResolvedValueOnce({ data: { albumId: 'al2', done: true } })
      render(<EditAlbumButton record={record} />)
      open()
      await screen.findByText('One')
      number('mf1', '3')
      type('album-name', 'Record')
      save()
      expect(
        await screen.findByText(/resources.album.editor.errors.unknown/),
      ).toBeTruthy()

      save()
      await waitFor(() =>
        expect(mockRedirect).toHaveBeenCalledWith('/album/al2/show'),
      )
      expect(mockCorrectTrackNumbers).toHaveBeenCalledTimes(1)
      expect(mockCorrectAlbum).toHaveBeenCalledTimes(2)
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
})
