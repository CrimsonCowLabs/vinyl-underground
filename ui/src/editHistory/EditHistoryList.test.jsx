import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  EditChangesField,
  EditStatusField,
  EditTrackField,
  UndoEditButton,
} from './EditHistoryList'

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
    Confirm: ({ isOpen, onConfirm, content }) =>
      isOpen ? (
        <div>
          <span>{content}</span>
          <button onClick={onConfirm}>confirm</button>
        </div>
      ) : null,
    useDataProvider: () => ({ undoEdit: mockUndo }),
    useNotify: () => mockNotify,
    useRefresh: () => mockRefresh,
    // A field label falls back to the tag's own name, as react-admin's `_` option does.
    useTranslate: () => (key, opts) =>
      key === 'resources.song.tagEditor.fields.title'
        ? 'Title'
        : opts?._ !== undefined
          ? opts._
          : key,
  }
})

const entry = {
  id: 'e1',
  kind: 'tags',
  state: 'written',
  path: 'Artist/Album/01 Song.mp3',
  track: { id: 't1', title: 'Song (Live)', artist: 'The Band' },
  changes: [
    { tag: 'TITLE', old: ['Song'], new: ['Song (Live)'] },
    { tag: 'REPLAYGAIN_X', old: [], new: ['1 dB'] },
    { tag: 'GENRE', old: ['Rock', 'Pop'], new: [] },
    { tag: 'COMMENT', old: [''], new: ['c'] },
  ],
  createdAt: '2026-11-22T10:00:00Z',
}

describe('Edit history page', () => {
  beforeEach(() => vi.clearAllMocks())

  it("shows each field's old and new values, by the field's name", () => {
    render(<EditChangesField record={entry} />)
    expect(screen.getByText('Title')).toBeTruthy()
    expect(screen.getByText('Song')).toBeTruthy()
    expect(screen.getByText('Song (Live)')).toBeTruthy()
    // A tag the dialog doesn't show keeps its own name.
    expect(screen.getByText('REPLAYGAIN_X')).toBeTruthy()
    // Several values are listed; none means the tag wasn't there, or was removed.
    expect(screen.getByText('Rock; Pop')).toBeTruthy()
    expect(
      screen.getAllByText('resources.editHistory.message.none'),
    ).toHaveLength(3)
  })

  it('shows the Track, and the File when the Track is gone', () => {
    const { rerender } = render(<EditTrackField record={entry} />)
    expect(screen.getByText('Song (Live)')).toBeTruthy()
    expect(screen.getByText('The Band')).toBeTruthy()
    expect(
      screen.queryByText('resources.editHistory.message.trackMissing'),
    ).toBeNull()
    rerender(
      <EditTrackField
        record={{ ...entry, track: { ...entry.track, missing: true } }}
      />,
    )
    expect(
      screen.getByText('resources.editHistory.message.trackMissing'),
    ).toBeTruthy()
    rerender(<EditTrackField record={{ ...entry, track: undefined }} />)
    expect(
      screen.getByText('resources.editHistory.message.trackGone'),
    ).toBeTruthy()
  })

  it('marks an undo, and a write that failed', () => {
    const { rerender } = render(
      <EditStatusField record={{ ...entry, undoOf: 'e0' }} />,
    )
    expect(screen.getByText('resources.editHistory.status.undo')).toBeTruthy()
    rerender(
      <EditStatusField record={{ ...entry, undoOf: 'u1', redo: true }} />,
    )
    expect(screen.getByText('resources.editHistory.status.redo')).toBeTruthy()
    rerender(
      <EditStatusField
        record={{
          ...entry,
          state: 'failed',
          error: 'the copy did not check out',
        }}
      />,
    )
    expect(screen.getByText('resources.editHistory.status.failed')).toBeTruthy()
  })

  it('undoes an entry after a confirmation, and refreshes the list', async () => {
    mockUndo.mockResolvedValue({ data: { changed: true } })
    render(<UndoEditButton record={entry} />)
    fireEvent.click(screen.getByText('resources.editHistory.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))
    await waitFor(() => expect(mockUndo).toHaveBeenCalledWith('e1'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.editHistory.notifications.undone',
        { type: 'info' },
      ),
    )
    expect(mockRefresh).toHaveBeenCalled()
  })

  it.each([
    'changedLater',
    'changedOnDisk',
    'tagsChanged',
    'alreadyUndone',
    'unfinished',
  ])('explains a refused undo (%s)', async (code) => {
    mockUndo.mockRejectedValue({ body: { error: code } })
    render(<UndoEditButton record={entry} />)
    fireEvent.click(screen.getByText('resources.editHistory.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        `resources.editHistory.errors.${code}`,
        { type: 'warning' },
      ),
    )
  })

  it("can't undo a write that never happened", () => {
    render(<UndoEditButton record={{ ...entry, state: 'failed' }} />)
    expect(
      screen.getByText('resources.editHistory.actions.undo').closest('button')
        .disabled,
    ).toBe(true)
  })
})
