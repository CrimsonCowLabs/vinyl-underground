import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { BulkEditDialog } from './BulkEditDialog'

const { mockFields, mockPreview, mockRun, mockNotify } = vi.hoisted(() => ({
  mockFields: vi.fn(),
  mockPreview: vi.fn(),
  mockRun: vi.fn(),
  mockNotify: vi.fn(),
}))

vi.mock('./bulkEdit', async (importOriginal) => ({
  ...(await importOriginal()),
  getBulkFields: mockFields,
  previewBulkEdit: mockPreview,
  runBulkEdit: mockRun,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useNotify: () => mockNotify,
  useTranslate: () => (key, opts) =>
    opts && Object.keys(opts).length ? `${key} ${JSON.stringify(opts)}` : key,
}))

const fields = [
  { key: 'albumartist', type: 'text', multi: false },
  { key: 'genre', type: 'text', multi: true },
]

const preview = {
  total: 3,
  changes: 1,
  unchanged: 1,
  refused: { unsupportedFormat: 1 },
  rows: [
    {
      id: 't1',
      title: 'One',
      path: 'A/01 One.mp3',
      old: [],
      new: ['The Band'],
      status: 'change',
    },
    {
      id: 't2',
      title: 'Two',
      path: 'A/02 Two.mp3',
      old: ['The Band'],
      new: ['The Band'],
      status: 'unchanged',
    },
    {
      id: 't3',
      title: 'Three',
      path: 'A/03 Three.flac',
      old: [],
      new: ['The Band'],
      status: 'unsupportedFormat',
    },
  ],
}

const renderDialog = (props = {}) => {
  const onDone = vi.fn()
  render(
    <BulkEditDialog
      open
      selection={{ filter: { folder: 'A' } }}
      count={3}
      onClose={vi.fn()}
      onDone={onDone}
      {...props}
    />,
  )
  return { onDone }
}

// Picks the field and types its value.
const setField = async (key, value) => {
  const select = await screen.findByTestId('bulk-edit-field')
  fireEvent.change(select, { target: { value: key } })
  if (value !== undefined) {
    fireEvent.change(await screen.findByTestId(`tag-${key}-0`), {
      target: { value },
    })
  }
}

describe('BulkEditDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFields.mockResolvedValue(fields)
    mockPreview.mockResolvedValue(preview)
  })

  it('previews every Track, old to new, and why a File is left alone', async () => {
    renderDialog()
    await setField('albumartist', ' The Band ')
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))

    await screen.findByTestId('bulk-edit-row-t1')
    expect(mockPreview).toHaveBeenCalledWith(
      { filter: { folder: 'A' }, field: 'albumartist', values: ['The Band'] },
      0,
      100,
    )
    expect(screen.getByTestId('bulk-edit-summary').textContent).toContain(
      '"smart_count":1,"total":3,"unchanged":1,"refused":1',
    )
    expect(screen.getByTestId('bulk-edit-row-t1').textContent).toContain(
      'The Band',
    )
    expect(screen.getByTestId('bulk-edit-row-t2').textContent).toContain(
      'resources.libraryTrack.bulkEdit.unchanged',
    )
    expect(screen.getByTestId('bulk-edit-row-t3').textContent).toContain(
      'resources.song.tagEditor.errors.unsupportedFormat',
    )
  })

  it('sets a multi-valued field as a list, and clears a field', async () => {
    renderDialog({ selection: { ids: ['t1', 't2'] } })
    await setField('genre', 'Rock')
    fireEvent.click(screen.getByTestId('tag-genre-add'))
    fireEvent.change(screen.getByTestId('tag-genre-1'), {
      target: { value: 'Pop' },
    })
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    await waitFor(() =>
      expect(mockPreview).toHaveBeenCalledWith(
        { ids: ['t1', 't2'], field: 'genre', values: ['Rock', 'Pop'] },
        0,
        100,
      ),
    )

    fireEvent.click(await screen.findByText('ra.action.back'))
    fireEvent.click(screen.getByTestId('bulk-edit-clear'))
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    await waitFor(() =>
      expect(mockPreview).toHaveBeenLastCalledWith(
        { ids: ['t1', 't2'], field: 'genre', values: [] },
        0,
        100,
      ),
    )
  })

  it("doesn't take an empty value for clearing the field", async () => {
    renderDialog()
    await setField('albumartist')
    expect(screen.getByTestId('bulk-edit-preview')).toBeDisabled()
    fireEvent.change(screen.getByTestId('tag-albumartist-0'), {
      target: { value: '  ' },
    })
    expect(screen.getByTestId('bulk-edit-preview')).toBeDisabled()
    fireEvent.click(screen.getByTestId('bulk-edit-clear'))
    expect(screen.getByTestId('bulk-edit-preview')).toBeEnabled()
  })

  it('runs every File that may change, including those that seem to hold the value', async () => {
    renderDialog()
    await setField('albumartist', 'The Band')
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    expect((await screen.findByTestId('bulk-edit-run')).textContent).toContain(
      '"smart_count":2',
    )
  })

  it('runs the edit as one job, and says so when it is done', async () => {
    mockRun.mockResolvedValue({
      job: { state: 'done', done: 1, skipped: 0 },
      background: false,
    })
    const { onDone } = renderDialog()
    await setField('albumartist', 'The Band')
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    fireEvent.click(await screen.findByTestId('bulk-edit-run'))

    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(mockRun).toHaveBeenCalledWith({
      filter: { folder: 'A' },
      field: 'albumartist',
      values: ['The Band'],
    })
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.libraryTrack.bulkEdit.done',
      { type: 'info', messageArgs: { smart_count: 1 } },
    )
  })

  it('says the job runs in the background when the server stopped waiting', async () => {
    mockRun.mockResolvedValue({ job: { state: 'running' }, background: true })
    const { onDone } = renderDialog()
    await setField('albumartist', 'The Band')
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    fireEvent.click(await screen.findByTestId('bulk-edit-run'))

    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.libraryTrack.bulkEdit.inBackground',
      { type: 'info' },
    )
  })

  it("explains a refusal, and can't run an edit that changes nothing", async () => {
    mockPreview.mockResolvedValueOnce({ ...preview, changes: 0, unchanged: 0 })
    renderDialog()
    await setField('albumartist', 'The Band')
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    expect(await screen.findByTestId('bulk-edit-run')).toBeDisabled()

    fireEvent.click(screen.getByText('ra.action.back'))
    mockPreview.mockRejectedValueOnce({ body: { error: 'tooMany' } })
    fireEvent.click(screen.getByTestId('bulk-edit-preview'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.libraryTrack.bulkEdit.errors.tooMany',
        { type: 'warning' },
      ),
    )
  })
})
