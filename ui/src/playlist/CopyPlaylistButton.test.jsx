import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { CopyPlaylistButton } from './CopyPlaylistButton'

const notify = vi.fn()
const redirect = vi.fn()
const refresh = vi.fn()
const httpClient = vi.fn()

vi.mock('react-admin', () => ({
  Button: ({ label, onClick, disabled, children }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
      {label}
    </button>
  ),
  useTranslate: () => (key) => key,
  useNotify: () => notify,
  useRedirect: () => redirect,
  useRefresh: () => refresh,
}))

vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const sharedPlaylist = { id: 'pls-shared', ownerId: 'owner-1', public: true }

describe('<CopyPlaylistButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    localStorage.setItem('userId', 'listener-2')
  })

  it("is offered on another Listener's shared Playlist", () => {
    render(<CopyPlaylistButton record={sharedPlaylist} />)
    expect(
      screen.getByText('resources.playlist.actions.copyToMyPlaylists'),
    ).toBeTruthy()
  })

  it('is not offered on your own Playlist', () => {
    localStorage.setItem('userId', 'owner-1')
    const { container } = render(<CopyPlaylistButton record={sharedPlaylist} />)
    expect(container.textContent).toBe('')
  })

  it('copies the Playlist and opens the new copy', async () => {
    httpClient.mockResolvedValue({ json: { id: 'pls-copy' } })
    render(<CopyPlaylistButton record={sharedPlaylist} />)

    fireEvent.click(
      screen.getByText('resources.playlist.actions.copyToMyPlaylists'),
    )

    await waitFor(() => {
      expect(redirect).toHaveBeenCalledWith('/playlist/pls-copy/show')
    })
    expect(httpClient).toHaveBeenCalledWith('/api/playlist/pls-shared/copy', {
      method: 'POST',
    })
    expect(notify).toHaveBeenCalledWith('resources.playlist.message.copied', {
      type: 'info',
    })
    expect(refresh).toHaveBeenCalled()
  })

  it('reports an error when the copy fails', async () => {
    httpClient.mockRejectedValue(new Error('boom'))
    render(<CopyPlaylistButton record={sharedPlaylist} />)

    fireEvent.click(
      screen.getByText('resources.playlist.actions.copyToMyPlaylists'),
    )

    await waitFor(() => {
      expect(notify).toHaveBeenCalledWith('ra.page.error', { type: 'warning' })
    })
    expect(redirect).not.toHaveBeenCalled()
  })
})
