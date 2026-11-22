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

describe('Corrections page', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows what a Correction changes, from and to', () => {
    render(<CorrectionChangeField record={rename} />)
    expect(screen.getByText('Greatful Dead')).toBeTruthy()
    expect(screen.getByText('Grateful Dead')).toBeTruthy()
  })

  it('counts the Tracks a Correction applies to', () => {
    render(<CorrectionStatusField record={rename} />)
    expect(
      screen.getByText('resources.correction.status.tracks {"smart_count":12}'),
    ).toBeTruthy()
  })

  it('flags an orphaned Correction', () => {
    render(<CorrectionStatusField record={{ ...rename, trackCount: 0 }} />)
    expect(
      screen.getByText('resources.correction.status.orphaned'),
    ).toBeTruthy()
  })

  it('shows a Correction being undone', () => {
    render(<CorrectionStatusField record={{ ...rename, undoing: true }} />)
    expect(screen.getByText('resources.correction.status.undoing')).toBeTruthy()
  })

  it('undoes a Correction after confirming, then reloads the list', async () => {
    mockUndo.mockResolvedValue({ data: { id: 'c1', done: true } })
    render(<UndoCorrectionButton record={rename} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    expect(
      screen.getByText(/resources.correction.message.undoContent/),
    ).toBeTruthy()
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
    expect(mockUndo).toHaveBeenCalledWith('c1')
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.correction.notifications.undone',
      expect.anything(),
    )
  })

  it('says when an undo carries on in the background', async () => {
    mockUndo.mockResolvedValue({ data: { id: 'c1', done: false } })
    render(<UndoCorrectionButton record={rename} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.correction.notifications.undoing',
        expect.anything(),
      ),
    )
  })

  it('reports a failed undo', async () => {
    mockUndo.mockRejectedValue({ status: 500, body: { error: 'internal' } })
    render(<UndoCorrectionButton record={rename} />)
    fireEvent.click(screen.getByText('resources.correction.actions.undo'))
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('ra.page.error', {
        type: 'warning',
      }),
    )
  })
})
