import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ChooseCoverButton } from './ChooseCoverButton'

const {
  mockPermissions,
  mockGetCandidates,
  mockChoose,
  mockNotify,
  mockRefresh,
  mockDataProvider,
} = vi.hoisted(() => ({
  mockPermissions: { value: 'admin' },
  mockGetCandidates: vi.fn(),
  mockChoose: vi.fn(),
  mockNotify: vi.fn(),
  mockRefresh: vi.fn(),
  mockDataProvider: {},
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
    // Stable, as react-admin's is: the picker asks again when it changes
    useDataProvider: () => mockDataProvider,
    useNotify: () => mockNotify,
    useRefresh: () => mockRefresh,
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useTranslate: () => (key, opts) =>
      opts && !opts._ ? `${key} ${JSON.stringify(opts)}` : (opts?._ ?? key),
  }
})

const album = {
  id: 'al1',
  name: 'Record',
  mbzAlbumId: 'rel-1',
  mbzReleaseGroupId: 'rg-1',
}

const answers = {
  local: {
    candidates: [{ id: 'c-folder', source: 'folder', label: 'cover.jpg' }],
    failed: false,
  },
  external: { candidates: [], failed: true },
  coverartarchive: {
    candidates: [
      {
        id: 'c-exact',
        source: 'coverartarchive',
        label: 'Record',
        detail: 'GB, CD, 1999',
        exactRelease: true,
      },
      {
        id: 'c-other',
        source: 'coverartarchive',
        label: 'Record (Deluxe)',
        detail: 'US, 2009',
      },
    ],
    failed: false,
  },
}

const open = () =>
  fireEvent.click(screen.getByText('resources.album.actions.chooseCover'))

describe('ChooseCoverButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPermissions.value = 'admin'
    mockDataProvider.getCoverCandidates = mockGetCandidates
    mockDataProvider.chooseAlbumCover = mockChoose
    mockGetCandidates.mockImplementation((albumId, source) =>
      Promise.resolve({ data: answers[source] }),
    )
  })

  it('is not offered to regular users', () => {
    mockPermissions.value = 'user'
    render(<ChooseCoverButton record={album} />)
    expect(screen.queryByText('resources.album.actions.chooseCover')).toBeNull()
  })

  it('lists every source’s candidates, labelled, with a preview of each', async () => {
    render(<ChooseCoverButton record={album} />)
    open()

    await screen.findByText('cover.jpg')
    expect(await screen.findByText('Record (Deluxe)')).toBeTruthy()
    expect(screen.getByText('GB, CD, 1999')).toBeTruthy()
    expect(
      screen.getByText('resources.album.coverPicker.exactRelease'),
    ).toBeTruthy()
    expect(mockGetCandidates).toHaveBeenCalledWith('al1', 'local')
    expect(mockGetCandidates).toHaveBeenCalledWith('al1', 'external')
    expect(mockGetCandidates).toHaveBeenCalledWith('al1', 'coverartarchive')
    const img = screen.getByAltText('cover.jpg')
    expect(img.getAttribute('src')).toContain(
      '/api/albumCover/al1/candidates/c-folder/preview',
    )
  })

  it('shows the sources that answered when another fails or never answers', async () => {
    mockGetCandidates.mockImplementation((albumId, source) => {
      if (source === 'coverartarchive') return new Promise(() => {})
      if (source === 'external') return Promise.reject(new Error('down'))
      return Promise.resolve({ data: answers[source] })
    })
    render(<ChooseCoverButton record={album} />)
    open()

    expect(await screen.findByText('cover.jpg')).toBeTruthy()
    expect(
      await screen.findByText('resources.album.coverPicker.failed'),
    ).toBeTruthy()
  })

  it('only asks the Cover Art Archive for albums with a MusicBrainz ID', async () => {
    render(<ChooseCoverButton record={{ id: 'al2', name: 'Demo' }} />)
    open()
    await screen.findByText('cover.jpg')
    expect(mockGetCandidates).not.toHaveBeenCalledWith('al2', 'coverartarchive')
    expect(
      screen.queryByText('resources.album.coverPicker.sources.coverartarchive'),
    ).toBeNull()
  })

  it('makes the chosen candidate the album’s cover', async () => {
    mockChoose.mockResolvedValue({ data: { id: 'cr1', done: true } })
    render(<ChooseCoverButton record={album} />)
    open()

    fireEvent.click(await screen.findByAltText('Record (Deluxe)'))
    fireEvent.click(screen.getByText('resources.album.coverPicker.use'))

    await waitFor(() =>
      expect(mockChoose).toHaveBeenCalledWith('al1', 'c-other'),
    )
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.album.coverPicker.chosen',
        expect.anything(),
      ),
    )
    expect(mockRefresh).toHaveBeenCalled()
    expect(screen.queryByText('resources.album.coverPicker.use')).toBeNull()
  })

  it('explains a choice that failed, and stays open', async () => {
    mockChoose.mockRejectedValue({ body: { error: 'unavailable' } })
    render(<ChooseCoverButton record={album} />)
    open()

    fireEvent.click(await screen.findByAltText('cover.jpg'))
    fireEvent.click(screen.getByText('resources.album.coverPicker.use'))

    expect(
      await screen.findByText('resources.correction.errors.unavailable'),
    ).toBeTruthy()
    expect(screen.getByText('resources.album.coverPicker.use')).toBeTruthy()
  })
})
