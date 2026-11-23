import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import QuarantineList from './QuarantineList'
import { formatSize } from './quarantine'

const { mockGet, mockRestore, mockEmpty, mockNotify } = vi.hoisted(() => ({
  mockGet: vi.fn(),
  mockRestore: vi.fn(),
  mockEmpty: vi.fn(),
  mockNotify: vi.fn(),
}))

let editing

vi.mock('./quarantine', async (importOriginal) => ({
  ...(await importOriginal()),
  getQuarantine: mockGet,
  restoreFromQuarantine: mockRestore,
  emptyQuarantine: mockEmpty,
}))

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => editing,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  Title: () => null,
  useNotify: () => mockNotify,
  useTranslate: () => (key, opts) =>
    opts && Object.keys(opts).length ? `${key} ${JSON.stringify(opts)}` : key,
}))

const contents = {
  count: 2,
  size: 3 * 1024 * 1024,
  emptyCount: 2,
  emptySize: 3 * 1024 * 1024,
  files: [
    {
      libraryId: 1,
      library: 'NAS',
      path: '_Quarantine/Artist/Album/01 Song.mp3',
      size: 2 * 1024 * 1024,
      writable: true,
      original: 'Artist/Album/01 Song.mp3',
      entryId: 'e1',
      trackId: 't1',
      title: 'Song',
      artist: 'Artist',
    },
    {
      libraryId: 1,
      library: 'NAS',
      path: '_Quarantine/Loose/Dupe 1.mp3',
      size: 1024 * 1024,
      writable: true,
      original: 'Loose/Dupe 1.mp3',
    },
  ],
}

describe('<QuarantineList />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    editing = { loaded: true, enabled: true }
    mockGet.mockResolvedValue(contents)
  })

  it('lists every File in Quarantine, where it came from and its Track, with their count and size', async () => {
    render(<QuarantineList />)
    await waitFor(() =>
      expect(
        screen.getByText('_Quarantine/Artist/Album/01 Song.mp3'),
      ).toBeTruthy(),
    )
    expect(screen.getByText('Artist/Album/01 Song.mp3')).toBeTruthy()
    expect(screen.getByText('Song')).toBeTruthy()
    expect(
      screen.getByText('resources.quarantine.message.noTrack'),
    ).toBeTruthy()
    expect(
      screen.getByText(
        'resources.quarantine.message.summary {"smart_count":2,"size":"3.0 MB"}',
      ),
    ).toBeTruthy()
  })

  it('restores one File, and lists Quarantine again', async () => {
    mockRestore.mockResolvedValue({ job: {}, background: false })
    render(<QuarantineList />)
    await waitFor(() => screen.getByText('Artist/Album/01 Song.mp3'))
    fireEvent.click(
      screen.getAllByText('resources.quarantine.actions.restore')[1],
    )
    await waitFor(() =>
      expect(mockRestore).toHaveBeenCalledWith(contents.files[1]),
    )
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(2))
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.quarantine.notifications.restored',
      { type: 'info', messageArgs: { path: 'Loose/Dupe 1.mp3' } },
    )
  })

  it("empties Quarantine only after a confirmation stating how many Files, how much space, and that it can't be undone", async () => {
    mockEmpty.mockResolvedValue({ job: { skipped: 0, steps: [] } })
    render(<QuarantineList />)
    await waitFor(() => screen.getByText('Artist/Album/01 Song.mp3'))
    fireEvent.click(screen.getByText('resources.quarantine.actions.empty'))
    expect(mockEmpty).not.toHaveBeenCalled()
    expect(
      screen.getByText(
        'resources.quarantine.empty.content {"smart_count":2,"size":"3.0 MB"}',
      ),
    ).toBeTruthy()
    expect(
      screen.getByText('resources.quarantine.empty.cannotUndo'),
    ).toBeTruthy()
    fireEvent.click(
      screen.getByText('resources.quarantine.empty.confirm {"smart_count":2}'),
    )
    await waitFor(() =>
      expect(mockEmpty).toHaveBeenCalledWith(
        expect.objectContaining({ count: 2, size: 3 * 1024 * 1024 }),
      ),
    )
  })

  it("can't restore or empty while the library is read-only", async () => {
    mockGet.mockResolvedValue({
      ...contents,
      emptyCount: 0,
      emptySize: 0,
      files: contents.files.map((f) => ({ ...f, writable: false })),
    })
    render(<QuarantineList />)
    await waitFor(() => screen.getByText('Artist/Album/01 Song.mp3'))
    for (const b of screen.getAllByText(
      'resources.quarantine.actions.restore',
    )) {
      expect(b.closest('button').disabled).toBe(true)
    }
    expect(
      screen.getByText('resources.quarantine.actions.empty').closest('button')
        .disabled,
    ).toBe(true)
  })

  it('says which Files Empty Quarantine left, and why', async () => {
    mockEmpty.mockResolvedValue({
      job: {
        skipped: 0,
        steps: [{ state: 'written', warning: '1 Files changed since' }],
      },
    })
    render(<QuarantineList />)
    await waitFor(() => screen.getByText('Artist/Album/01 Song.mp3'))
    fireEvent.click(screen.getByText('resources.quarantine.actions.empty'))
    fireEvent.click(
      screen.getByText('resources.quarantine.empty.confirm {"smart_count":2}'),
    )
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.quarantine.notifications.left',
        { type: 'warning', messageArgs: { warning: '1 Files changed since' } },
      ),
    )
  })

  it('says when Quarantine is empty', async () => {
    mockGet.mockResolvedValue({ count: 0, size: 0, files: [] })
    render(<QuarantineList />)
    await waitFor(() =>
      expect(screen.getByText('resources.quarantine.empty.none')).toBeTruthy(),
    )
  })
})

describe('formatSize', () => {
  it('reads as people read sizes', () => {
    expect(formatSize(0)).toBe('0 bytes')
    expect(formatSize(512)).toBe('512 bytes')
    expect(formatSize(1536)).toBe('1.5 KB')
    expect(formatSize(5 * 1024 ** 3)).toBe('5.0 GB')
  })
})
