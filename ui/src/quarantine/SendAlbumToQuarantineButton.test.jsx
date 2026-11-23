import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SendAlbumToQuarantineButton } from './SendAlbumToQuarantineButton'

const { mockSend, mockRedirect } = vi.hoisted(() => ({
  mockSend: vi.fn(),
  mockRedirect: vi.fn(),
}))

let editing

vi.mock('./quarantine', async (importOriginal) => ({
  ...(await importOriginal()),
  sendToQuarantine: mockSend,
}))

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => editing,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  Button: ({ children, onClick, label }) => (
    <button onClick={onClick}>
      {label}
      {children}
    </button>
  ),
  useNotify: () => vi.fn(),
  useRedirect: () => mockRedirect,
  useTranslate: () => (key) => key,
}))

const album = { id: 'al-1', libraryId: 1, songCount: 12 }

describe('<SendAlbumToQuarantineButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    editing = { enabled: true, isWritable: (id) => id === 1 }
  })

  it("sends every Track of the album to Quarantine after a confirmation, then leaves the album's page", async () => {
    mockSend.mockResolvedValue({ job: { skipped: 0 }, background: false })
    render(<SendAlbumToQuarantineButton record={album} />)
    fireEvent.click(screen.getByText('resources.quarantine.actions.sendAlbum'))
    fireEvent.click(screen.getByText('resources.quarantine.send.confirm'))
    await waitFor(() => expect(mockRedirect).toHaveBeenCalledWith('/album'))
    expect(mockSend).toHaveBeenCalledWith({ albumId: 'al-1' })
  })

  it('is hidden while editing is off, and for an album in a read-only library', () => {
    editing = { enabled: false, isWritable: () => false }
    const { rerender } = render(<SendAlbumToQuarantineButton record={album} />)
    expect(
      screen.queryByText('resources.quarantine.actions.sendAlbum'),
    ).toBeNull()
    editing = { enabled: true, isWritable: () => false }
    rerender(<SendAlbumToQuarantineButton record={{ ...album }} />)
    expect(
      screen.queryByText('resources.quarantine.actions.sendAlbum'),
    ).toBeNull()
  })
})
