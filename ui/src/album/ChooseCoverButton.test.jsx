import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ChooseCoverButton } from './ChooseCoverButton'

const {
  mockPermissions,
  mockGetCandidates,
  mockChoose,
  mockBlock,
  mockUpload,
  mockGetPreview,
  mockNotify,
  mockRefresh,
  mockDataProvider,
  mockEmbed,
  mockWritable,
} = vi.hoisted(() => ({
  mockEmbed: vi.fn(),
  mockWritable: { value: true },
  mockPermissions: { value: 'admin' },
  mockGetCandidates: vi.fn(),
  mockChoose: vi.fn(),
  mockBlock: vi.fn(),
  mockUpload: vi.fn(),
  mockGetPreview: vi.fn(),
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

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => ({
    // undefined: the server hasn't answered yet
    loaded: mockWritable.value !== undefined,
    enabled: true,
    isWritable: () => mockWritable.value,
  }),
}))

vi.mock('../subsonic', () => ({
  default: {
    getCoverArtUrl: (record) => `/cover/al-${record.id}_${record.imageHash}`,
  },
}))

const album = {
  id: 'al1',
  name: 'Record',
  albumArtist: 'Band',
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

// A preview as the data provider answers it, as react-admin wants: { data: Blob }. The Blob
// carries its candidate's ID, which names its blob URL.
const preview = (candidateId) =>
  Promise.resolve({
    data: Object.assign(new Blob(['img'], { type: 'image/png' }), {
      candidateId,
    }),
  })

const open = () =>
  fireEvent.click(screen.getByText('resources.album.actions.chooseCover'))

describe('ChooseCoverButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPermissions.value = 'admin'
    mockWritable.value = true
    mockDataProvider.embedAlbumCover = mockEmbed
    mockDataProvider.getCoverCandidates = mockGetCandidates
    mockDataProvider.chooseAlbumCover = mockChoose
    mockDataProvider.blockAlbumCover = mockBlock
    mockDataProvider.uploadAlbumCover = mockUpload
    mockDataProvider.getCoverPreview = mockGetPreview
    mockGetCandidates.mockImplementation((albumId, source) =>
      Promise.resolve({ data: answers[source] }),
    )
    // Each preview is a Blob, shown from a blob URL named after it
    // react-admin's useDataProvider passes on only (resource, payload)
    mockGetPreview.mockImplementation((albumId, { candidateId }) =>
      preview(candidateId),
    )
    URL.createObjectURL = vi.fn((blob) => `blob:${blob.candidateId}`)
    URL.revokeObjectURL = vi.fn()
  })

  afterEach(() => {
    localStorage.removeItem('token')
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
    const img = await screen.findByAltText('cover.jpg')
    expect(img.getAttribute('src')).toBe('blob:c-folder')
    expect(mockGetPreview).toHaveBeenCalledWith('al1', {
      candidateId: 'c-folder',
      signal: expect.any(AbortSignal),
    })
  })

  describe('previews', () => {
    it('never put the login token in a URL', async () => {
      localStorage.setItem('token', 'secret-token')
      render(<ChooseCoverButton record={album} />)
      open()
      await screen.findByAltText('Record (Deluxe)')
      for (const img of document.querySelectorAll('img')) {
        expect(img.getAttribute('src')).not.toContain('secret-token')
        expect(img.getAttribute('src')).not.toContain('jwt')
      }
    })

    it('show each as it arrives, without waiting for a slow one', async () => {
      mockGetPreview.mockImplementation((albumId, { candidateId }) =>
        candidateId === 'c-exact'
          ? new Promise(() => {})
          : preview(candidateId),
      )
      render(<ChooseCoverButton record={album} />)
      open()

      expect(
        (await screen.findByAltText('cover.jpg')).getAttribute('src'),
      ).toBe('blob:c-folder')
      expect(
        (await screen.findByAltText('Record (Deluxe)')).getAttribute('src'),
      ).toBe('blob:c-other')
      // The slow one can still be chosen while it loads
      expect(screen.getByLabelText('Record')).toBeTruthy()
    })

    it('are released when the picker closes', async () => {
      render(<ChooseCoverButton record={album} />)
      open()
      await screen.findByAltText('cover.jpg')
      await screen.findByAltText('Record (Deluxe)')
      await screen.findByAltText('Record')

      fireEvent.click(screen.getByText('ra.action.cancel'))

      await waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledTimes(3))
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:c-folder')
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:c-exact')
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:c-other')
    })

    it('that arrive after the picker closed are dropped, and the rest cancelled', async () => {
      let arrive
      const signals = []
      mockGetPreview.mockImplementation((albumId, { candidateId, signal }) => {
        signals.push(signal)
        return new Promise((resolve) => {
          arrive = () => resolve(preview(candidateId))
        })
      })
      render(<ChooseCoverButton record={album} />)
      open()
      await screen.findByText('Record (Deluxe)')

      fireEvent.click(screen.getByText('ra.action.cancel'))
      arrive()
      await new Promise((r) => setTimeout(r, 0))

      expect(signals.every((s) => s.aborted)).toBe(true)
      expect(URL.createObjectURL).not.toHaveBeenCalled()
    })

    it('show a placeholder for one that failed, which can still be chosen', async () => {
      mockGetPreview.mockImplementation((albumId, { candidateId }) =>
        candidateId === 'c-folder'
          ? Promise.reject(new Error('not an image'))
          : preview(candidateId),
      )
      render(<ChooseCoverButton record={album} />)
      open()

      await screen.findByAltText('Record (Deluxe)')
      expect(screen.queryByAltText('cover.jpg')).toBeNull()
      expect(screen.getByLabelText('cover.jpg')).toBeTruthy()
    })
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

  it('says when the cover write carries on in the background', async () => {
    mockChoose.mockResolvedValue({ data: { id: 'job-1', background: true } })
    render(<ChooseCoverButton record={album} />)
    open()
    fireEvent.click(await screen.findByAltText('cover.jpg'))
    fireEvent.click(screen.getByText('resources.album.coverPicker.use'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.album.coverPicker.writingInBackground',
        expect.anything(),
      ),
    )
  })

  it('passes on a warning that another image still outranks the cover', async () => {
    mockChoose.mockResolvedValue({
      data: { id: 'job-1', changed: true, warning: 'folder.jpg wins.' },
    })
    render(<ChooseCoverButton record={album} />)
    open()
    fireEvent.click(await screen.findByAltText('cover.jpg'))
    fireEvent.click(screen.getByText('resources.album.coverPicker.use'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.album.coverPicker.writtenWarning',
        expect.objectContaining({
          type: 'warning',
          messageArgs: { warning: 'folder.jpg wins.' },
        }),
      ),
    )
  })

  it('explains a cover write the server refused', async () => {
    mockChoose.mockRejectedValue({ body: { error: 'sharedFolder' } })
    render(<ChooseCoverButton record={album} />)
    open()
    fireEvent.click(await screen.findByAltText('cover.jpg'))
    fireEvent.click(screen.getByText('resources.album.coverPicker.use'))
    expect(
      await screen.findByText(
        'resources.album.coverPicker.errors.sharedFolder',
      ),
    ).toBeTruthy()
  })

  it('writes no cover while the album’s library is read-only, but can still mark it wrong', async () => {
    mockWritable.value = false
    render(
      <ChooseCoverButton
        record={{ ...album, imageHash: '0123456789abcdef' }}
      />,
    )
    open()
    fireEvent.click(await screen.findByAltText('cover.jpg'))
    expect(
      screen.getByText('resources.album.coverPicker.editingOff'),
    ).toBeTruthy()
    expect(
      screen.getByText('resources.album.coverPicker.use').closest('button')
        .disabled,
    ).toBe(true)
    expect(
      screen.getByLabelText('resources.album.coverPicker.upload').disabled,
    ).toBe(true)
    expect(
      screen.getByText('resources.album.coverPicker.embed').closest('button')
        .disabled,
    ).toBe(true)
    expect(
      screen
        .getByText('resources.album.coverPicker.markWrong')
        .closest('button').disabled,
    ).toBe(false)
  })

  it('says nothing of a read-only library while it isn’t known yet', async () => {
    mockWritable.value = undefined
    render(<ChooseCoverButton record={album} />)
    open()
    await screen.findByAltText('cover.jpg')
    expect(
      screen.queryByText('resources.album.coverPicker.editingOff'),
    ).toBeNull()
  })

  describe('Embed in Files', () => {
    const embed = async () => {
      render(<ChooseCoverButton record={album} />)
      open()
      fireEvent.click(screen.getByText('resources.album.coverPicker.embed'))
      // It asks first: it rewrites every File
      expect(
        await screen.findByText('resources.album.coverPicker.embedContent'),
      ).toBeTruthy()
      expect(mockEmbed).not.toHaveBeenCalled()
      fireEvent.click(screen.getByText('ra.action.confirm'))
      await waitFor(() => expect(mockEmbed).toHaveBeenCalledWith('al1'))
    }

    it('embeds the cover in the album’s Files once confirmed', async () => {
      mockEmbed.mockResolvedValue({
        data: { id: 'job-2', state: 'done', total: 3, skipped: 0 },
      })
      await embed()
      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.coverPicker.embedded',
          expect.anything(),
        ),
      )
    })

    it('says how many Files were skipped', async () => {
      mockEmbed.mockResolvedValue({
        data: { id: 'job-2', state: 'done', total: 3, skipped: 1 },
      })
      await embed()
      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.coverPicker.embeddedSkipped',
          expect.objectContaining({ messageArgs: { smart_count: 1 } }),
        ),
      )
    })

    it('explains an album with no folder cover yet', async () => {
      mockEmbed.mockRejectedValue({ body: { error: 'noFolderCover' } })
      await embed()
      expect(
        await screen.findByText(
          'resources.album.coverPicker.errors.noFolderCover',
        ),
      ).toBeTruthy()
    })
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

  describe('the current cover', () => {
    const shown = { ...album, imageHash: '0123456789abcdef' }

    it('shows the album’s cover, and marks it wrong', async () => {
      mockBlock.mockResolvedValue({ data: { id: 'cr2', done: true } })
      render(<ChooseCoverButton record={shown} />)
      open()

      const img = screen.getByAltText('resources.album.coverPicker.current')
      expect(img.getAttribute('src')).toContain('al-al1_0123456789abcdef')
      fireEvent.click(screen.getByText('resources.album.coverPicker.markWrong'))

      await waitFor(() =>
        expect(mockBlock).toHaveBeenCalledWith('al1', '0123456789abcdef'),
      )
      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.coverPicker.markedWrong',
          expect.anything(),
        ),
      )
      expect(mockRefresh).toHaveBeenCalled()
      expect(screen.queryByText('resources.album.coverPicker.use')).toBeNull()
    })

    it('can’t be marked wrong when the album has no cover', () => {
      render(<ChooseCoverButton record={album} />)
      open()
      expect(
        screen
          .getByText('resources.album.coverPicker.markWrong')
          .closest('button').disabled,
      ).toBe(true)
    })

    it('explains a cover already marked wrong', async () => {
      mockBlock.mockRejectedValue({ body: { error: 'alreadyCorrected' } })
      render(<ChooseCoverButton record={shown} />)
      open()
      fireEvent.click(screen.getByText('resources.album.coverPicker.markWrong'))
      expect(
        await screen.findByText('resources.correction.errors.alreadyBlocked'),
      ).toBeTruthy()
    })

    it('explains when the cover has changed since', async () => {
      mockBlock.mockRejectedValue({ body: { error: 'coverChanged' } })
      render(<ChooseCoverButton record={shown} />)
      open()
      fireEvent.click(screen.getByText('resources.album.coverPicker.markWrong'))
      expect(
        await screen.findByText('resources.correction.errors.coverChanged'),
      ).toBeTruthy()
    })
  })

  describe('uploading a cover', () => {
    const file = new File(['png'], 'front.png', { type: 'image/png' })
    const pick = () =>
      fireEvent.change(
        screen.getByLabelText('resources.album.coverPicker.upload'),
        { target: { files: [file] } },
      )

    it('makes the uploaded image the album’s cover', async () => {
      mockUpload.mockResolvedValue({ data: { id: 'cr3', done: true } })
      render(<ChooseCoverButton record={album} />)
      open()
      pick()

      await waitFor(() => expect(mockUpload).toHaveBeenCalledWith('al1', file))
      await waitFor(() =>
        expect(mockNotify).toHaveBeenCalledWith(
          'resources.album.coverPicker.uploaded',
          expect.anything(),
        ),
      )
      expect(mockRefresh).toHaveBeenCalled()
    })

    it('explains an upload refused as too large, and stays open', async () => {
      mockUpload.mockRejectedValue({ body: { error: 'tooLarge' } })
      render(<ChooseCoverButton record={album} />)
      open()
      pick()

      expect(
        await screen.findByText('resources.correction.errors.tooLarge'),
      ).toBeTruthy()
      expect(screen.getByText('resources.album.coverPicker.use')).toBeTruthy()
    })
  })
})
