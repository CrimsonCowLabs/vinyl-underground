import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { DownloadPlaylistButton } from './DownloadPlaylistButton'

const notify = vi.fn()
let download
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

vi.mock('../download', () => ({
  downloads,
  downloadsSupported: () => true,
  useDownload: () => download,
}))

const playlist = { id: 'pl1', name: 'Road trip' }

describe('<DownloadPlaylistButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    download = undefined
  })

  it('downloads a Playlist that is not on the device', () => {
    render(<DownloadPlaylistButton record={playlist} />)

    fireEvent.click(screen.getByText('resources.playlist.actions.download'))

    expect(downloads.start).toHaveBeenCalledWith('pl1')
  })

  it('tells the Listener when the Download could not start', async () => {
    downloads.start.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    render(<DownloadPlaylistButton record={playlist} />)

    fireEvent.click(screen.getByText('resources.playlist.actions.download'))

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'resources.playlist.message.downloadFailed',
        'warning',
      ),
    )
  })

  it('shows the progress of a Download', () => {
    download = { state: 'downloading', done: 2, total: 5 }
    render(<DownloadPlaylistButton record={playlist} />)

    expect(
      screen.getByText('resources.playlist.message.downloading 2/5'),
    ).toBeTruthy()
  })

  it('resumes an interrupted Download', () => {
    download = { state: 'interrupted', done: 2, total: 5 }
    render(<DownloadPlaylistButton record={playlist} />)

    fireEvent.click(
      screen.getByText('resources.playlist.actions.resumeDownload 2/5'),
    )

    expect(downloads.resume).toHaveBeenCalled()
  })

  it('removes a Download once the Listener confirms', async () => {
    download = { state: 'downloaded', done: 5, total: 5 }
    render(<DownloadPlaylistButton record={playlist} />)

    fireEvent.click(screen.getByText('resources.playlist.message.downloaded'))
    expect(downloads.remove).not.toHaveBeenCalled()
    fireEvent.click(
      screen.getByText('resources.playlist.actions.removeDownload'),
    )

    expect(downloads.remove).toHaveBeenCalledWith('pl1')
  })
})
