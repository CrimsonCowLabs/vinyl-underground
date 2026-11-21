import React from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { DownloadsStorage } from './DownloadsStorage'

let usage
vi.mock('../download', () => ({
  downloadsSupported: () => true,
  useDownloadUsage: () => usage,
}))

vi.mock('react-admin', () => ({
  useTranslate: () => (key, options) =>
    options ? `${key} ${Object.values(options).join(' ')}` : key,
}))

const MB = 1024 * 1024

const renderMeter = () =>
  render(
    <MemoryRouter>
      <DownloadsStorage />
    </MemoryRouter>,
  )

describe('<DownloadsStorage />', () => {
  beforeEach(() => {
    usage = undefined
  })

  it('shows the total and each Download, largest first', () => {
    usage = {
      total: 150 * MB,
      downloads: [
        { id: 'pl1', kind: 'playlist', name: 'Road trip', size: 60 * MB },
        { id: 'al1', kind: 'album', name: 'Blue', size: 90 * MB },
      ],
      device: { used: 160 * MB, quota: 1024 * MB },
    }

    renderMeter()

    expect(
      screen.getByText('download.storage.total 150 MB'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('download.storage.device 160 MB 1 GB'),
    ).toBeInTheDocument()
    const names = screen.getAllByRole('button').map((row) => row.textContent)
    expect(names).toEqual([
      'Bluedownload.kind.album90 MB',
      'Road tripdownload.kind.playlist60 MB',
    ])
    expect(screen.getByText('Blue').closest('a')).toHaveAttribute(
      'href',
      '/album/al1/show',
    )
  })

  it('says when nothing is downloaded', () => {
    usage = { total: 0, downloads: [] }

    renderMeter()

    expect(screen.getByText('download.storage.none')).toBeInTheDocument()
  })
})
