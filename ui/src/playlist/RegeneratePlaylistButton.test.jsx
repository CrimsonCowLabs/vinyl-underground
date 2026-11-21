import React from 'react'
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import config from '../config'
import { RegeneratePlaylistButton } from './RegeneratePlaylistButton'

const notify = vi.fn()
const redirect = vi.fn()
const refresh = vi.fn()
const httpClient = vi.fn()

vi.mock('../config', () => ({
  default: { enableGeneratedPlaylists: true },
}))

vi.mock('react-admin', () => ({
  Button: ({ label, onClick, disabled, children }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
      {label}
    </button>
  ),
  useTranslate: () => (key, opts) =>
    opts && opts.smart_count !== undefined
      ? `${key}:${opts.smart_count}`
      : opts && opts.prompt !== undefined
        ? `${key}:${opts.prompt}`
        : key,
  useNotify: () => notify,
  useRedirect: () => redirect,
  useRefresh: () => refresh,
}))

vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const generated = {
  id: 'pls-1',
  name: 'Rainy Sunday',
  ownerId: 'listener-1',
  prompt: 'rainy Sunday 90s indie',
  generatedLength: 12,
}

const preview = {
  prompt: 'rainy Sunday 90s indie',
  length: 12,
  name: 'Sunday Rain',
  tracks: [
    { id: 'mf-3', title: 'Alison', artist: 'Slowdive', year: 1993 },
    { id: 'mf-2', title: 'Velouria', artist: 'Pixies', year: 1990 },
  ],
}

const httpError = (status, code) =>
  Object.assign(new Error(code), { status, body: { error: code } })

const renderButton = (record = generated) =>
  render(
    <ThemeProvider theme={createTheme()}>
      <RegeneratePlaylistButton record={record} />
    </ThemeProvider>,
  )

const regenerateButton = () =>
  screen.getByRole('button', {
    name: 'resources.playlist.actions.regeneratePlaylist',
  })

const open = (record) => {
  renderButton(record)
  fireEvent.click(regenerateButton())
}

const replaceButton = () =>
  screen.queryByRole('button', {
    name: 'resources.playlist.actions.replaceTracks',
  })
const saveAsNewButton = () =>
  screen.getByRole('button', { name: 'resources.playlist.actions.saveAsNew' })

describe('<RegeneratePlaylistButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    config.enableGeneratedPlaylists = true
    localStorage.setItem('userId', 'listener-1')
  })
  afterEach(() => {
    cleanup()
    localStorage.removeItem('userId')
  })

  it('is not offered on an ordinary Playlist', () => {
    const { container } = renderButton({ ...generated, prompt: undefined })
    expect(container.textContent).toBe('')
  })

  it('is hidden when no LLM is configured', () => {
    config.enableGeneratedPlaylists = false
    const { container } = renderButton()
    expect(container.textContent).toBe('')
  })

  it('previews the stored Prompt again as soon as it opens', async () => {
    let resolve
    httpClient.mockReturnValue(new Promise((r) => (resolve = r)))
    open()

    expect(httpClient).toHaveBeenCalledWith(
      '/api/generatedPlaylist/pls-1/preview',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(
      screen.getByText(
        'resources.playlist.message.generatedFrom:rainy Sunday 90s indie',
      ),
    ).toBeTruthy()
    expect(screen.getByRole('progressbar')).toBeTruthy()

    resolve({ json: preview })
    await waitFor(() => screen.getByText('Alison'))
    expect(screen.getByText('Velouria')).toBeTruthy()
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it("replaces the Playlist's Tracks with the preview", async () => {
    httpClient
      .mockResolvedValueOnce({ json: preview })
      .mockResolvedValueOnce({ json: { id: 'pls-1' } })
    open()
    await waitFor(() => screen.getByText('Alison'))

    fireEvent.click(replaceButton())

    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(httpClient).toHaveBeenLastCalledWith(
      '/api/generatedPlaylist/pls-1/tracks',
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ trackIds: ['mf-3', 'mf-2'] }),
      }),
    )
    expect(notify).toHaveBeenCalledWith(
      'resources.playlist.message.generatedReplaced',
      { type: 'info' },
    )
    expect(redirect).not.toHaveBeenCalled()
    expect(screen.queryByText('Alison')).toBeNull()
  })

  it('saves the preview as a new Playlist and opens it', async () => {
    httpClient
      .mockResolvedValueOnce({ json: preview })
      .mockResolvedValueOnce({ json: { id: 'pls-2' } })
    open()
    await waitFor(() => screen.getByText('Alison'))

    fireEvent.click(saveAsNewButton())

    await waitFor(() =>
      expect(redirect).toHaveBeenCalledWith('/playlist/pls-2/show'),
    )
    expect(httpClient).toHaveBeenLastCalledWith(
      '/api/generatedPlaylist',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          name: 'Sunday Rain',
          prompt: 'rainy Sunday 90s indie',
          length: 12,
          trackIds: ['mf-3', 'mf-2'],
        }),
      }),
    )
  })

  // A shared Playlist is read-only to other Listeners
  it("only offers to save another Listener's Playlist as a new one", async () => {
    httpClient.mockResolvedValueOnce({ json: preview })
    open({ ...generated, ownerId: 'listener-2' })
    await waitFor(() => screen.getByText('Alison'))

    expect(replaceButton()).toBeNull()
    expect(saveAsNewButton()).toBeTruthy()
  })

  it('tries again, and explains a failure', async () => {
    httpClient.mockRejectedValueOnce(httpError(503, 'aiUnavailable'))
    open()
    await waitFor(() =>
      screen.getByText('resources.playlist.message.aiUnavailable'),
    )
    expect(replaceButton()).toBeNull()

    httpClient.mockResolvedValueOnce({ json: preview })
    fireEvent.click(
      screen.getByRole('button', {
        name: 'resources.playlist.actions.regenerate',
      }),
    )
    await waitFor(() => screen.getByText('Alison'))
    expect(httpClient).toHaveBeenCalledTimes(2)
  })

  // e.g. the Playlist was deleted, or stopped being shared, while open
  it('explains why the Tracks could not be replaced', async () => {
    httpClient
      .mockResolvedValueOnce({ json: preview })
      .mockRejectedValueOnce(httpError(403, 'notAuthorized'))
    open()
    await waitFor(() => screen.getByText('Alison'))

    fireEvent.click(replaceButton())

    await waitFor(() =>
      screen.getByText('resources.playlist.message.notAuthorized'),
    )
    expect(refresh).not.toHaveBeenCalled()
    expect(replaceButton().disabled).toBe(false)
  })

  it('cancels the request when the dialog is closed', () => {
    httpClient.mockReturnValue(new Promise(() => {}))
    open()
    const { signal } = httpClient.mock.calls[0][1]
    expect(signal.aborted).toBe(false)

    fireEvent.click(screen.getByText('ra.action.cancel'))

    expect(signal.aborted).toBe(true)
  })
})
