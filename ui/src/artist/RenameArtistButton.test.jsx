import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { RenameArtistButton } from './RenameArtistButton'

const { mockPermissions, mockRenameArtist, mockNotify, mockRedirect } =
  vi.hoisted(() => ({
    mockPermissions: { value: 'admin' },
    mockRenameArtist: vi.fn(),
    mockNotify: vi.fn(),
    mockRedirect: vi.fn(),
  }))

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
    useDataProvider: () => ({ renameArtist: mockRenameArtist }),
    useNotify: () => mockNotify,
    useRedirect: () => mockRedirect,
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

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
  })

  it('is not offered to regular users', () => {
    mockPermissions.value = 'user'
    render(<RenameArtistButton record={record} />)
    expect(screen.queryByText('resources.artist.actions.rename')).toBeNull()
  })

  it('opens with the current name filled in', () => {
    render(<RenameArtistButton record={record} />)
    fireEvent.click(screen.getByText('resources.artist.actions.rename'))
    expect(screen.getByRole('textbox').value).toBe('Greatful Dead')
  })

  it('saves the rename and opens the renamed Artist once the rescan is done', async () => {
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

  it('closes once saved when the rescan carries on in the background', async () => {
    mockRenameArtist.mockResolvedValue({
      data: { artistId: 'ar2', done: false },
    })
    render(<RenameArtistButton record={record} />)
    rename('Grateful Dead')

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.artist.notifications.renaming',
        expect.anything(),
      ),
    )
    expect(mockRedirect).not.toHaveBeenCalled()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('explains a refused rename and stays open', async () => {
    mockRenameArtist.mockRejectedValue({
      status: 409,
      body: { code: 'artistExists' },
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
