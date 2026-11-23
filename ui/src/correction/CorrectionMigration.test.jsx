import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import CorrectionMigration from './CorrectionMigration'

const { mockPlan, mockMigrate, mockNotify, mockRefresh, dataProvider } =
  vi.hoisted(() => {
    const mockPlan = vi.fn()
    const mockMigrate = vi.fn()
    return {
      mockPlan,
      mockMigrate,
      mockNotify: vi.fn(),
      mockRefresh: vi.fn(),
      // As react-admin's, the same from one render to the next.
      dataProvider: {
        getCorrectionMigration: mockPlan,
        migrateCorrections: mockMigrate,
      },
    }
  })

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
    useDataProvider: () => dataProvider,
    useNotify: () => mockNotify,
    useRefresh: () => mockRefresh,
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

const plan = {
  files: 3,
  items: [
    {
      correction: {
        id: 'c1',
        kind: 'artistRename',
        from: 'Greatful Dead',
        to: 'Grateful Dead',
      },
      changes: [
        { field: 'artist', from: 'Greatful Dead', to: 'Grateful Dead' },
      ],
      files: 2,
      flags: [
        {
          path: 'Band/Record/03.flac',
          code: 'unsupportedFormat',
          error: 'this kind of File can’t be edited yet',
        },
      ],
    },
    {
      correction: {
        id: 'c2',
        kind: 'album',
        from: 'Band – Recrod',
        to: 'Band – Record',
      },
      changes: [{ field: 'album', to: 'Record' }],
      files: 2,
      flags: [],
      keepsRelease: true,
      lastRun: [{ path: 'Band/Record/01.mp3', code: 'changedOnDisk' }],
    },
  ],
}

describe('Correction migration', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows nothing when no Correction is left to write', async () => {
    mockPlan.mockResolvedValue({ data: { items: [], files: 0 } })
    const { container } = render(<CorrectionMigration />)
    await waitFor(() => expect(mockPlan).toHaveBeenCalled())
    expect(container.textContent).toBe('')
  })

  it('lists each Correction with what writing it changes, and flags the Files it cannot write', async () => {
    mockPlan.mockResolvedValue({ data: plan })
    render(<CorrectionMigration />)
    await screen.findByText('resources.correction.migration.title')
    expect(
      screen.getByText(
        /resources.correction.kinds.artistRename: Greatful Dead → Grateful Dead/,
      ),
    ).toBeTruthy()
    expect(
      screen.getByText(/resources.correction.migration.fields.artist/)
        .textContent,
    ).toContain('Greatful Dead → Grateful Dead')
    expect(
      screen.getByText(
        'Band/Record/03.flac: resources.correction.migration.codes.unsupportedFormat',
      ),
    ).toBeTruthy()
    expect(
      screen.getByText('resources.correction.migration.keepsRelease'),
    ).toBeTruthy()
    expect(
      screen.getByText(/resources.correction.migration.lastRun/),
    ).toBeTruthy()
    expect(
      screen.getByText(
        'Band/Record/01.mp3: resources.correction.migration.codes.changedOnDisk',
      ),
    ).toBeTruthy()
  })

  it('runs the migration once approved, says how it went, and reloads', async () => {
    mockPlan.mockResolvedValue({ data: plan })
    mockMigrate.mockResolvedValue({
      data: { done: true, job: { skipped: 1, steps: [] } },
    })
    render(<CorrectionMigration />)
    fireEvent.click(
      await screen.findByText(/resources.correction.migration.approve/),
    )
    expect(
      screen.getByText('resources.correction.migration.confirmContent'),
    ).toBeTruthy()
    fireEvent.click(screen.getByText('confirm'))

    await waitFor(() => expect(mockRefresh).toHaveBeenCalled())
    expect(mockMigrate).toHaveBeenCalled()
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.correction.migration.notifications.skipped',
      { type: 'warning', messageArgs: { smart_count: 1 } },
    )
    expect(mockPlan).toHaveBeenCalledTimes(2)
  })

  it('says when the migration carries on in the background', async () => {
    mockPlan.mockResolvedValue({ data: plan })
    mockMigrate.mockResolvedValue({ data: { background: true } })
    render(<CorrectionMigration />)
    fireEvent.click(
      await screen.findByText(/resources.correction.migration.approve/),
    )
    fireEvent.click(screen.getByText('confirm'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.correction.migration.notifications.background',
        { type: 'info' },
      ),
    )
  })

  it('explains a read-only Library', async () => {
    mockPlan.mockResolvedValue({ data: plan })
    mockMigrate.mockRejectedValue({
      status: 403,
      body: { error: 'editingOff' },
    })
    render(<CorrectionMigration />)
    fireEvent.click(
      await screen.findByText(/resources.correction.migration.approve/),
    )
    fireEvent.click(screen.getByText('confirm'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.correction.migration.notifications.editingOff',
        { type: 'warning' },
      ),
    )
  })
})
