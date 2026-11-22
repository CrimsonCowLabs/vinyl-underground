import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditAlbumButton } from './EditAlbumButton'

const {
  mockPermissions,
  mockCorrectAlbum,
  mockGetList,
  mockNotify,
  mockRedirect,
} = vi.hoisted(() => ({
  mockPermissions: { value: 'admin' },
  mockCorrectAlbum: vi.fn(),
  mockGetList: vi.fn(),
  mockNotify: vi.fn(),
  mockRedirect: vi.fn(),
}))
// The dataProvider is the same object across renders, as react-admin's is
const mockDataProvider = {
  correctAlbum: mockCorrectAlbum,
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
    mockGetList.mockResolvedValue({
      data: [{ id: 'ar2', name: 'The Band' }],
    })
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
})
