import React from 'react'
import { act, fireEvent, render, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import UploadForm from './UploadForm'

const refresh = vi.fn()
vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    useRefresh: () => refresh,
    useTranslate: () => (key, opts) =>
      opts && Object.keys(opts).some((k) => k !== '_')
        ? `${key} ${JSON.stringify(opts)}`
        : key,
  }
})

const pick = (input, ...names) =>
  fireEvent.change(input, {
    target: { files: names.map((n) => new File(['x'], n)) },
  })

describe('UploadForm', () => {
  it('takes several Files at once, and shows each one’s progress as it uploads', async () => {
    let report
    let finish
    const upload = vi.fn(
      (files, { onChange }) =>
        new Promise((resolve) => {
          report = onChange
          finish = resolve
        }),
    )
    const { getByTestId, getByRole } = render(<UploadForm upload={upload} />)
    const input = getByTestId('upload-input')
    expect(input.multiple).toBe(true)
    expect(input.accept).toContain('.flac')

    pick(input, 'a.mp3', 'b.flac')
    expect(upload.mock.calls[0][0].map((f) => f.name)).toEqual([
      'a.mp3',
      'b.flac',
    ])
    act(() =>
      report([
        { name: 'a.mp3', status: 'uploading', progress: 0.42 },
        { name: 'b.flac', status: 'waiting', progress: 0 },
      ]),
    )
    expect(getByTestId('upload-0').textContent).toContain(
      'resources.rip.upload.progress {"percent":42}',
    )
    expect(getByRole('progressbar').getAttribute('aria-valuenow')).toBe('42')
    expect(getByTestId('upload-1').textContent).toContain(
      'resources.rip.upload.waiting',
    )

    const done = [
      { name: 'a.mp3', status: 'done', progress: 1 },
      {
        name: 'b.flac',
        status: 'failed',
        error: "b.flac: it doesn't read as FLAC audio",
      },
    ]
    await act(async () => {
      report(done)
      finish(done)
    })
    expect(getByTestId('upload-0').textContent).toContain(
      'resources.rip.upload.done',
    )
    expect(getByTestId('upload-1').textContent).toContain(
      "b.flac: it doesn't read as FLAC audio",
    )
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })
})
