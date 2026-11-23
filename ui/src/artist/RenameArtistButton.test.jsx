import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { RenameArtistButton } from './RenameArtistButton'

const {
  mockPermissions,
  mockRenameArtist,
  mockMergeArtist,
  mockGetList,
  mockNotify,
  mockRedirect,
  mockEditing,
} = vi.hoisted(() => ({
  mockEditing: { enabled: true },
  mockPermissions: { value: 'admin' },
  mockRenameArtist: vi.fn(),
  mockMergeArtist: vi.fn(),
  mockGetList: vi.fn(),
  mockNotify: vi.fn(),
  mockRedirect: vi.fn(),
}))
// The dataProvider is the same object across renders, as react-admin's is
const mockDataProvider = {
  renameArtist: mockRenameArtist,
  mergeArtist: mockMergeArtist,
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

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => ({
    loaded: true,
    enabled: mockEditing.enabled,
    isWritable: () => mockEditing.enabled,
  }),
}))

const record = { id: 'ar1', name: 'Greatful Dead' }

const rename = (name) => {
  fireEvent.click(screen.getByText('resources.artist.actions.rename'))
  fireEvent.change(screen.getByRole('textbox'), { target: { value: name } })
  fireEvent.click(screen.getByText('ra.action.save'))
}

describe('RenameArtistButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPermissions.value = 'admin'
    mockEditing.enabled = true
    mockGetList.mockResolvedValue({ data: [] })
  })

  it('is not offered to regular users', () => {
    mockPermissions.value = 'user'
    render(<RenameArtistButton record={record} />)
    expect(screen.queryByText('resources.artist.actions.rename')).toBeNull()
  })

  it("is not offered while editing the Library's Files is off", () => {
    mockEditing.enabled = false
    render(<RenameArtistButton record={record} />)
    expect(screen.queryByText('resources.artist.actions.rename')).toBeNull()
  })

  it('says how many Files the rename job skipped', async () => {
    mockRenameArtist.mockResolvedValue({
      data: { artistId: 'ar2', done: true, job: { skipped: 1 } },
    })
    render(<RenameArtistButton record={record} />)
    rename('Grateful Dead')

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.artist.notifications.skipped',
        { type: 'warning', messageArgs: { smart_count: 1 } },
      ),
    )
    expect(mockRedirect).toHaveBeenCalledWith('/artist/ar2/show')
  })

  it('opens with the current name filled in', () => {
    render(<RenameArtistButton record={record} />)
    fireEvent.click(screen.getByText('resources.artist.actions.rename'))
    expect(screen.getByRole('textbox').value).toBe('Greatful Dead')
  })

  it('writes the rename to the Files and opens the renamed Artist once the job is done', async () => {
    mockRenameArtist.mockResolvedValue({
      data: { artistId: 'ar2', done: true },
    })
    render(<RenameArtistButton record={record} />)
    rename('Grateful Dead')

    await waitFor(() =>
      expect(mockRedirect).toHaveBeenCalledWith('/artist/ar2/show'),
    )
    expect(mockRenameArtist).toHaveBeenCalledWith('ar1', 'Grateful Dead')
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.artist.notifications.renamed',
      expect.anything(),
    )
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('closes once saved, back to the Artists, when the job carries on in the background', async () => {
    mockRenameArtist.mockResolvedValue({
      data: { jobId: 'job-1', background: true, done: false },
    })
    render(<RenameArtistButton record={record} />)
    rename('Grateful Dead')

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.artist.notifications.renaming',
        expect.anything(),
      ),
    )
    expect(mockRedirect).toHaveBeenCalledWith('/artist')
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('suggests existing Artists as you type', async () => {
    mockGetList.mockResolvedValue({
      data: [
        { id: 'ar1', name: 'Greatful Dead' },
        { id: 'ar2', name: 'Grateful Dead', albumCount: 12 },
      ],
    })
    render(<RenameArtistButton record={record} />)
    fireEvent.click(screen.getByText('resources.artist.actions.rename'))
    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'Grat' },
    })

    expect(await screen.findByText('Grateful Dead')).toBeTruthy()
    expect(mockGetList).toHaveBeenLastCalledWith(
      'artist',
      expect.objectContaining({ filter: { name: 'Grat' } }),
    )
    // Not the Artist being renamed
    expect(screen.queryByRole('option', { name: 'Greatful Dead' })).toBeNull()
    expect(screen.queryByText(/mergeWarning/)).toBeNull()
  })

  it('warns, naming the Artist and its albums, before merging into an existing Artist', async () => {
    mockGetList.mockResolvedValue({
      data: [{ id: 'ar2', name: 'Grateful Dead', albumCount: 12 }],
    })
    mockMergeArtist.mockResolvedValue({
      data: { artistId: 'ar2', done: true },
    })
    render(<RenameArtistButton record={record} />)
    fireEvent.click(screen.getByText('resources.artist.actions.rename'))
    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'grateful  dead' },
    })

    expect(
      await screen.findByText(
        'resources.artist.message.mergeWarning {"name":"Grateful Dead","smart_count":12}',
      ),
    ).toBeTruthy()
    fireEvent.click(screen.getByText('resources.artist.actions.merge'))

    await waitFor(() =>
      expect(mockRedirect).toHaveBeenCalledWith('/artist/ar2/show'),
    )
    expect(mockMergeArtist).toHaveBeenCalledWith('ar1', 'ar2')
    expect(mockRenameArtist).not.toHaveBeenCalled()
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.artist.notifications.merged',
      expect.anything(),
    )
  })

  it('explains a refused rename and stays open', async () => {
    mockRenameArtist.mockRejectedValue({
      status: 409,
      body: { error: 'artistExists' },
    })
    render(<RenameArtistButton record={record} />)
    rename('Grateful Dead')

    expect(
      await screen.findByText(
        'resources.correction.errors.artistExists {"name":"Grateful Dead"}',
      ),
    ).toBeTruthy()
    expect(screen.getByRole('textbox')).toBeTruthy()
  })
})
