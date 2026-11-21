import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { OfflineGate } from './OfflineGate'
import { PLAYER_PLAY_TRACKS } from '../actions'

const dispatch = vi.fn()
const notify = vi.fn()
let offline
let downloaded

vi.mock('react-redux', () => ({ useDispatch: () => dispatch }))

vi.mock('react-admin', () => ({
  Button: ({ label, onClick }) => <button onClick={onClick}>{label}</button>,
  useTranslate: () => (key) => key,
  useNotify: () => notify,
}))

vi.mock('./index', async () => {
  const { isAvailableOffline } = await import('./offline')
  return {
    downloadsSupported: () => true,
    isAvailableOffline,
    useIsOffline: () => offline,
    downloadedTracksForPlayer: async () => downloaded,
  }
})

const renderAt = (path) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <OfflineGate>
        <p>the page</p>
      </OfflineGate>
    </MemoryRouter>,
  )

describe('<OfflineGate />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    offline = true
    downloaded = {}
  })

  it('shows every page while online', () => {
    offline = false
    renderAt('/artist')

    expect(screen.getByText('the page')).toBeInTheDocument()
  })

  it('shows a downloaded Playlist offline', () => {
    renderAt('/playlist/pl1/show')

    expect(screen.getByText('the page')).toBeInTheDocument()
  })

  it('says a page that needs the server is unavailable offline', () => {
    renderAt('/artist')

    expect(screen.queryByText('the page')).not.toBeInTheDocument()
    expect(
      screen.getByText('download.offline.unavailableTitle'),
    ).toBeInTheDocument()
  })

  it('offers to shuffle all Downloads instead', async () => {
    downloaded = { t1: { id: 't1', title: 'T1' } }
    renderAt('/artist')

    fireEvent.click(screen.getByText('download.actions.shuffleAll'))

    await waitFor(() =>
      expect(dispatch).toHaveBeenCalledWith(
        expect.objectContaining({
          type: PLAYER_PLAY_TRACKS,
          data: { _t1: { id: 't1', title: 'T1' } },
        }),
      ),
    )
  })

  it('says when there is nothing downloaded to shuffle', async () => {
    renderAt('/artist')

    fireEvent.click(screen.getByText('download.actions.shuffleAll'))

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'download.message.nothingToShuffle',
        'info',
      ),
    )
    expect(dispatch).not.toHaveBeenCalled()
  })
})
