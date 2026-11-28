import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditTagsDialog } from './EditTagsDialog'

const {
  mockGetTrackTags,
  mockEditTrackTags,
  mockGetList,
  mockNotify,
  mockRefresh,
} = vi.hoisted(() => ({
  mockGetTrackTags: vi.fn(),
  mockGetList: vi.fn(),
  mockEditTrackTags: vi.fn(),
  mockNotify: vi.fn(),
  mockRefresh: vi.fn(),
}))
const mockDataProvider = {
  getTrackTags: mockGetTrackTags,
  editTrackTags: mockEditTrackTags,
  getList: mockGetList,
}

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useDataProvider: () => mockDataProvider,
    useNotify: () => mockNotify,
    useRefresh: () => mockRefresh,
    useTranslate: () => (key) => key,
  }
})

const tags = {
  id: 'mf1',
  path: 'Artist/Album/02 Song.mp3',
  fields: [
    { key: 'title', type: 'text', multi: false },
    { key: 'genre', type: 'text', multi: true },
    { key: 'tracknumber', type: 'number', multi: false },
    { key: 'compilation', type: 'bool', multi: false },
    { key: 'lyrics', type: 'longtext', multi: false },
  ],
  values: {
    title: ['Song'],
    genre: ['Rock'],
    tracknumber: ['2'],
    compilation: [],
    lyrics: [],
  },
}

const input = (key, i = 0) => screen.getByTestId(`tag-${key}-${i}`)
const type = (key, value, i = 0) =>
  fireEvent.change(input(key, i), { target: { value } })
const save = () => fireEvent.click(screen.getByText('ra.action.save'))

describe('EditTagsDialog', () => {
  const onClose = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    mockGetTrackTags.mockResolvedValue({ data: tags })
    mockEditTrackTags.mockResolvedValue({ data: { changed: true } })
    mockGetList.mockResolvedValue({
      data: [
        { id: 'g1', name: 'Rock' },
        { id: 'g2', name: 'Shoegaze' },
      ],
    })
  })

  const open = async (record = { id: 'mf1', title: 'Song' }) => {
    render(<EditTagsDialog record={record} onClose={onClose} />)
    await waitFor(() => input('title'))
  }

  it("shows the File's own tags, read from it", async () => {
    await open({ id: 'pt9', mediaFileId: 'mf1', title: 'Song' })
    expect(mockGetTrackTags).toHaveBeenCalledWith('mf1')
    expect(input('title').value).toBe('Song')
    expect(input('genre').value).toBe('Rock')
    expect(input('tracknumber').value).toBe('2')
    expect(screen.getByText('Artist/Album/02 Song.mp3')).toBeTruthy()
  })

  it("suggests the Library's genres as a genre is typed", async () => {
    await open()
    expect(mockGetList).toHaveBeenCalledWith(
      'genre',
      expect.objectContaining({ pagination: { page: 1, perPage: 0 } }),
    )
    type('genre', 'shoe')
    fireEvent.click(await screen.findByText('Shoegaze'))
    expect(input('genre').value).toBe('Shoegaze')
    save()
    await waitFor(() =>
      expect(mockEditTrackTags).toHaveBeenCalledWith('mf1', {
        genre: ['Shoegaze'],
      }),
    )
  })

  it('says when a genre is not in the Library yet, and still saves it', async () => {
    await open()
    await waitFor(() => expect(mockGetList).toHaveBeenCalled())
    expect(screen.queryByText('resources.song.tagEditor.newValue')).toBeNull()
    type('genre', 'Shoegase')
    expect(
      await screen.findByText('resources.song.tagEditor.newValue'),
    ).toBeTruthy()
    save()
    await waitFor(() =>
      expect(mockEditTrackTags).toHaveBeenCalledWith('mf1', {
        genre: ['Shoegase'],
      }),
    )
  })

  it('saves only the fields that changed, a multi-valued one as a list', async () => {
    await open()
    type('title', 'Song (Live)')
    fireEvent.click(screen.getByTestId('tag-genre-add'))
    type('genre', 'Pop', 1)
    save()
    await waitFor(() => expect(mockEditTrackTags).toHaveBeenCalled())
    expect(mockEditTrackTags).toHaveBeenCalledWith('mf1', {
      title: ['Song (Live)'],
      genre: ['Rock', 'Pop'],
    })
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.song.tagEditor.saved',
      expect.anything(),
    )
    expect(mockRefresh).toHaveBeenCalled()
  })

  it('removes a value of a multi-valued field', async () => {
    mockGetTrackTags.mockResolvedValue({
      data: { ...tags, values: { ...tags.values, genre: ['Rock', 'Pop'] } },
    })
    await open()
    fireEvent.click(screen.getByTestId('tag-genre-remove-0'))
    save()
    await waitFor(() =>
      expect(mockEditTrackTags).toHaveBeenCalledWith('mf1', { genre: ['Pop'] }),
    )
  })

  it('lets the admin drop one of two values of a single-valued tag', async () => {
    mockGetTrackTags.mockResolvedValue({
      data: { ...tags, values: { ...tags.values, title: ['Song', 'Song 2'] } },
    })
    await open()
    fireEvent.click(screen.getByTestId('tag-title-remove-1'))
    save()
    await waitFor(() =>
      expect(mockEditTrackTags).toHaveBeenCalledWith('mf1', {
        title: ['Song'],
      }),
    )
  })

  it('says so when the File already held the tags, and nothing was written', async () => {
    mockEditTrackTags.mockResolvedValue({ data: { changed: false } })
    await open()
    type('title', 'Song (Live)')
    save()
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.song.tagEditor.nothingChanged',
        expect.anything(),
      ),
    )
  })

  it('closes and says the edit runs in the background when its job takes longer', async () => {
    mockEditTrackTags.mockResolvedValue({
      data: { jobId: 'job-1', background: true },
    })
    await open()
    type('title', 'Song (Live)')
    save()
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.song.tagEditor.savingInBackground',
      expect.objectContaining({ type: 'info' }),
    )
    expect(mockNotify).not.toHaveBeenCalledWith(
      'resources.song.tagEditor.saved',
      expect.anything(),
    )
    // The Track still has its old tags: nothing to refresh yet.
    expect(mockRefresh).not.toHaveBeenCalled()
  })

  it('tells the admin when a restart interrupted the edit', async () => {
    mockEditTrackTags.mockRejectedValue({ body: { error: 'interrupted' } })
    await open()
    type('title', 'x')
    save()
    await waitFor(() =>
      screen.getByText('resources.song.tagEditor.errors.interrupted'),
    )
  })

  it('does not save when nothing changed', async () => {
    await open()
    expect(screen.getByText('ra.action.save').closest('button').disabled).toBe(
      true,
    )
  })

  it('tells the admin when the File changed on disk, and keeps the dialog open', async () => {
    mockEditTrackTags.mockRejectedValue({ body: { error: 'changedOnDisk' } })
    await open()
    type('title', 'x')
    save()
    await waitFor(() =>
      screen.getByText('resources.song.tagEditor.errors.changedOnDisk'),
    )
    expect(onClose).not.toHaveBeenCalled()
  })

  it('says why it cannot load the tags', async () => {
    mockGetTrackTags.mockRejectedValue({ body: { error: 'unsupportedFormat' } })
    render(<EditTagsDialog record={{ id: 'mf1' }} onClose={onClose} />)
    await waitFor(() =>
      screen.getByText('resources.song.tagEditor.errors.unsupportedFormat'),
    )
  })
})
