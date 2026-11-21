import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { DownloadButton } from './DownloadButton'

const notify = vi.fn()
let download
const downloadFor = vi.fn(() => download)
const downloads = vi.hoisted(() => ({
  start: vi.fn(() => Promise.resolve()),
  resume: vi.fn(() => Promise.resolve()),
  remove: vi.fn(() => Promise.resolve()),
}))

vi.mock('react-admin', () => ({
  Button: ({ label, onClick, children }) => (
    <button onClick={onClick}>
      {children}
      {label}
    </button>
  ),
  Confirm: ({ isOpen, confirm, onConfirm }) =>
    isOpen ? <button onClick={onConfirm}>{confirm}</button> : null,
  useTranslate: () => (key, options) =>
    options?.total ? `${key} ${options.done}/${options.total}` : key,
  useNotify: () => notify,
}))

vi.mock('./index', () => ({
  downloads,
  downloadsSupported: () => true,
  useDownload: (id, kind) => downloadFor(id, kind),
}))

const playlist = { id: 'pl1', name: 'Road trip' }

describe('<DownloadButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    download = undefined
  })

  it('downloads a Playlist that is not on the device', () => {
    render(<DownloadButton record={playlist} kind="playlist" />)

    fireEvent.click(screen.getByText('download.actions.download'))

    expect(downloads.start).toHaveBeenCalledWith('pl1', 'playlist')
  })

  it('tells the Listener when the Download could not start', async () => {
    downloads.start.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    render(<DownloadButton record={playlist} kind="playlist" />)

    fireEvent.click(screen.getByText('download.actions.download'))

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'download.message.failed',
        'warning',
        { name: 'Road trip' },
      ),
    )
  })

  it('shows the progress of a Download', () => {
    download = { state: 'downloading', done: 2, total: 5 }
    render(<DownloadButton record={playlist} kind="playlist" />)

    expect(screen.getByText('download.message.downloading 2/5')).toBeTruthy()
  })

  it('resumes an interrupted Download', () => {
    download = { state: 'interrupted', done: 2, total: 5 }
    render(<DownloadButton record={playlist} kind="playlist" />)

    fireEvent.click(screen.getByText('download.actions.resume 2/5'))

    expect(downloads.resume).toHaveBeenCalled()
  })

  it('removes a Download once the Listener confirms', async () => {
    download = { state: 'downloaded', done: 5, total: 5 }
    render(<DownloadButton record={playlist} kind="playlist" />)

    fireEvent.click(screen.getByText('download.message.downloaded'))
    expect(downloads.remove).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('download.actions.remove'))

    expect(downloads.remove).toHaveBeenCalledWith('pl1', 'playlist')
  })

  it('downloads an album', () => {
    const album = { id: 'al1', name: 'Blue' }
    render(<DownloadButton record={album} kind="album" />)

    fireEvent.click(screen.getByText('download.actions.download'))

    expect(downloadFor).toHaveBeenCalledWith('al1', 'album')
    expect(downloads.start).toHaveBeenCalledWith('al1', 'album')
  })
})
