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
import { GeneratePlaylistButton } from './GeneratePlaylistButton'

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
    opts && opts.smart_count !== undefined ? `${key}:${opts.smart_count}` : key,
  useNotify: () => notify,
  useRedirect: () => redirect,
  useRefresh: () => refresh,
}))

vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const preview = {
  prompt: 'rainy Sunday 90s indie',
  name: 'Rainy Sunday',
  tracks: [
    { id: 'mf-2', title: 'Velouria', artist: 'Pixies', year: 1990 },
    { id: 'mf-1', title: 'Cut Your Hair', artist: 'Pavement', year: 1994 },
  ],
}

const httpError = (status, code) =>
  Object.assign(new Error(code), { status, body: { error: code } })

const renderButton = () =>
  render(
    <ThemeProvider theme={createTheme()}>
      <GeneratePlaylistButton />
    </ThemeProvider>,
  )

const open = () => {
  renderButton()
  fireEvent.click(screen.getByText('resources.playlist.actions.generate'))
}

const promptField = () =>
  screen.getByLabelText('resources.playlist.fields.prompt', { exact: false })
const lengthField = () =>
  screen.getByLabelText('resources.playlist.fields.length', { exact: false })
const generateButton = () =>
  screen.getByRole('button', {
    name: 'resources.playlist.actions.generatePreview',
  })

describe('<GeneratePlaylistButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    config.enableGeneratedPlaylists = true
  })
  afterEach(cleanup)

  it('is hidden when no LLM is configured', () => {
    config.enableGeneratedPlaylists = false
    const { container } = renderButton()
    expect(container.textContent).toBe('')
  })

  it('asks for a Prompt and a length of 25 by default', () => {
    open()
    expect(promptField().value).toBe('')
    expect(lengthField().value).toBe('25')
    expect(generateButton().disabled).toBe(true)
  })

  it('only accepts a length from 10 to 100', () => {
    open()
    fireEvent.change(promptField(), { target: { value: 'indie' } })
    expect(generateButton().disabled).toBe(false)
    fireEvent.change(lengthField(), { target: { value: '9' } })
    expect(generateButton().disabled).toBe(true)
    fireEvent.change(lengthField(), { target: { value: '101' } })
    expect(generateButton().disabled).toBe(true)
    fireEvent.change(lengthField(), { target: { value: '100' } })
    expect(generateButton().disabled).toBe(false)
  })

  it('shows progress while the LLM works, then the preview with a Save button', async () => {
    let resolve
    httpClient.mockReturnValue(new Promise((r) => (resolve = r)))
    open()
    fireEvent.change(promptField(), {
      target: { value: 'rainy Sunday 90s indie' },
    })
    fireEvent.change(lengthField(), { target: { value: '30' } })
    fireEvent.click(generateButton())

    expect(httpClient).toHaveBeenCalledWith(
      '/api/generatedPlaylist/preview',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ prompt: 'rainy Sunday 90s indie', length: 30 }),
      }),
    )
    expect(screen.getByRole('progressbar')).toBeTruthy()
    expect(
      screen.getByText('resources.playlist.message.generating', {
        exact: false,
      }),
    ).toBeTruthy()

    resolve({ json: preview })
    await waitFor(() => screen.getByText('Velouria'))
    expect(screen.getByText('Cut Your Hair')).toBeTruthy()
    expect(screen.queryByRole('progressbar')).toBeNull()
    expect(
      screen.getByLabelText('resources.playlist.fields.name', {
        exact: false,
      }).value,
    ).toBe('Rainy Sunday')
    expect(screen.getByRole('button', { name: 'ra.action.save' })).toBeTruthy()
  })

  it('saves the preview as a Playlist and opens it', async () => {
    httpClient
      .mockResolvedValueOnce({ json: preview })
      .mockResolvedValueOnce({ json: { id: 'pls-1' } })
    open()
    fireEvent.change(promptField(), {
      target: { value: 'rainy Sunday 90s indie' },
    })
    fireEvent.click(generateButton())
    await waitFor(() => screen.getByText('Velouria'))
    fireEvent.change(
      screen.getByLabelText('resources.playlist.fields.name', {
        exact: false,
      }),
      { target: { value: 'Sunday rain' } },
    )
    fireEvent.click(screen.getByRole('button', { name: 'ra.action.save' }))

    await waitFor(() =>
      expect(redirect).toHaveBeenCalledWith('/playlist/pls-1/show'),
    )
    expect(httpClient).toHaveBeenLastCalledWith(
      '/api/generatedPlaylist',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          name: 'Sunday rain',
          prompt: 'rainy Sunday 90s indie',
          trackIds: ['mf-2', 'mf-1'],
        }),
      }),
    )
    expect(refresh).toHaveBeenCalled()
  })

  it('says the AI is unavailable when the LLM cannot be reached', async () => {
    httpClient.mockRejectedValue(httpError(503, 'aiUnavailable'))
    open()
    fireEvent.change(promptField(), { target: { value: 'indie' } })
    fireEvent.click(generateButton())
    await waitFor(() =>
      screen.getByText('resources.playlist.message.aiUnavailable'),
    )
    expect(screen.queryByRole('progressbar')).toBeNull()
    // The Prompt is kept so the Listener can try again.
    expect(promptField().value).toBe('indie')
    expect(generateButton().disabled).toBe(false)
  })

  it('explains other failures', async () => {
    httpClient.mockRejectedValue(httpError(422, 'noTracks'))
    open()
    fireEvent.change(promptField(), { target: { value: 'indie' } })
    fireEvent.click(generateButton())
    await waitFor(() => screen.getByText('resources.playlist.message.noTracks'))

    httpClient.mockRejectedValue(new Error('Failed to fetch'))
    fireEvent.click(generateButton())
    await waitFor(() =>
      screen.getByText('resources.playlist.message.generateFailed'),
    )
  })
})
