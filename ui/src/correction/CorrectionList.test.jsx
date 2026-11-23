import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  CorrectionChangeField,
  CorrectionStatusField,
  UndoCorrectionButton,
} from './CorrectionList'

const { mockUndo, mockNotify, mockRefresh } = vi.hoisted(() => ({
  mockUndo: vi.fn(),
  mockNotify: vi.fn(),
  mockRefresh: vi.fn(),
}))

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    Button: ({ children, onClick, label, disabled }) => (
      <button onClick={onClick} disabled={disabled}>
        {label}
        {children}
      </button>
    ),
    Confirm: ({ isOpen, onConfirm, content, translateOptions }) =>
      isOpen ? (
        <div>
          <span>{`${content} ${JSON.stringify(translateOptions)}`}</span>
          <button onClick={onConfirm}>confirm</button>
        </div>
      ) : null,
    useDataProvider: () => ({ undoCorrection: mockUndo }),
    useNotify: () => mockNotify,
    useRefresh: () => mockRefresh,
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

const rename = {
  id: 'c1',
  kind: 'artistRename',
  from: 'Greatful Dead',
  to: 'Grateful Dead',
  createdBy: 'admin',
  trackCount: 12,
}
const block = {
  id: 'c2',
  kind: 'albumCoverBlock',
  from: 'Band – Record',
  to: 'cover.jpg',
  createdBy: 'admin',
  trackCount: 12,
}
const release = {
  id: 'c3',
  kind: 'album',
  from: 'Band – Record',
  to: 'MusicBrainz: Record (GB, CD, 1999)',
  mbzAlbumId: 'a7f8ed5e-1111-4a1b-9c1d-123456789abc',
  trackCount: 2,
}

describe('Corrections page', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows what a Correction changes, from and to', () => {
    render(<CorrectionChangeField record={rename} />)
    expect(screen.getByText('Greatful Dead')).toBeTruthy()
    expect(screen.getByText('Grateful Dead')).toBeTruthy()
  })

  it('counts the Tracks a cover block applies to', () => {
    render(<CorrectionStatusField record={block} />)
    expect(
      screen.getByText('resources.correction.status.tracks {"smart_count":12}'),
    ).toBeTruthy()
  })

  it('flags an orphaned cover block', () => {
    render(<CorrectionStatusField record={{ ...block, trackCount: 0 }} />)
    expect(
      screen.getByText('resources.correction.status.orphaned'),
    ).toBeTruthy()
  })

  it('marks a Correction still to be written into the Files, which has no Undo', () => {
    render(<CorrectionStatusField record={rename} />)
    expect(
      screen.getByText('resources.correction.status.toMigrate'),
    ).toBeTruthy()
    render(<UndoCorrectionButton record={rename} />)
    expect(screen.queryByText('resources.correction.actions.undo')).toBeNull()
    render(
      <UndoCorrectionButton
        record={{ ...release, albumName: 'Record', id: 'c4' }}
      />,
    )
    expect(screen.queryByText('resources.correction.actions.undo')).toBeNull()
  })

  it('undoes a cover block after confirming, then reloads the list', async () => {
    mockUndo.mockResolvedValue({ data: { id: 'c2', done: true } })
    render(<UndoCorrectionButton record={block} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    expect(
      screen.getByText(
        /resources.correction.message.undoContentAlbumCoverBlock .*cover.jpg/,
      ),
    ).toBeTruthy()
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
    expect(mockUndo).toHaveBeenCalledWith('c2')
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.correction.notifications.undone',
      expect.anything(),
    )
  })

  it('has no Undo for a picked MusicBrainz release, which the migration writes into the Files (#170)', () => {
    render(<CorrectionStatusField record={release} />)
    expect(
      screen.getByText('resources.correction.status.toMigrate'),
    ).toBeTruthy()
    render(<UndoCorrectionButton record={release} />)
    expect(screen.queryByText('resources.correction.actions.undo')).toBeNull()
  })

  it('reports a failed undo', async () => {
    mockUndo.mockRejectedValue({ status: 500, body: { error: 'internal' } })
    render(<UndoCorrectionButton record={block} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('ra.page.error', {
        type: 'warning',
      }),
    )
  })

  it('says a Correction the migration writes cannot be undone', async () => {
    mockUndo.mockRejectedValue({
      status: 400,
      body: { error: 'migrateFirst' },
    })
    render(<UndoCorrectionButton record={block} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.correction.errors.migrateFirst',
        { type: 'warning' },
      ),
    )
  })
})
