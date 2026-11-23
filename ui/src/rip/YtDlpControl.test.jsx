import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import YtDlpControl from './YtDlpControl.jsx'

const notify = vi.fn()
const http = vi.fn()

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
    useNotify: () => notify,
  }
})

vi.mock('../dataProvider', () => ({
  httpClient: (...args) => http(...args),
}))

const reply = (json) => Promise.resolve({ json })

const updateButton = () =>
  screen.getByRole('button', { name: /resources.rip.ytdlp.update/ })
const highlighted = (button) =>
  button.classList.contains('MuiButton-containedPrimary')

describe('YtDlpControl', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it("shows the installed version alone when the latest release isn't known", async () => {
    http.mockReturnValueOnce(reply({ version: '2026.10.01' }))
    render(<YtDlpControl />)

    expect(
      await screen.findByText(/resources.rip.ytdlp.version .*2026.10.01/),
    ).toBeInTheDocument()
    expect(http).toHaveBeenCalledWith('/api/rip/ytdlp')
    expect(highlighted(updateButton())).toBe(false)
  })

  it('says when a newer release is available and highlights Update', async () => {
    http.mockReturnValueOnce(
      reply({ version: '2026.10.01', latest: '2026.11.01' }),
    )
    render(<YtDlpControl />)

    expect(
      await screen.findByText(
        /resources.rip.ytdlp.versionAvailable.*2026.10.01.*2026.11.01/,
      ),
    ).toBeInTheDocument()
    expect(highlighted(updateButton())).toBe(true)
  })

  it('says when the installed version is the latest', async () => {
    http.mockReturnValueOnce(
      reply({ version: '2026.11.01', latest: '2026.11.01' }),
    )
    render(<YtDlpControl />)

    expect(
      await screen.findByText(
        /resources.rip.ytdlp.versionUpToDate.*2026.11.01/,
      ),
    ).toBeInTheDocument()
    expect(highlighted(updateButton())).toBe(false)
  })

  it('says which release Update will install when yt-dlp is not installed yet', async () => {
    http.mockReturnValueOnce(reply({ version: '', latest: '2026.11.01' }))
    render(<YtDlpControl />)

    expect(
      await screen.findByText(
        /resources.rip.ytdlp.notInstalledAvailable.*2026.11.01/,
      ),
    ).toBeInTheDocument()
    expect(highlighted(updateButton())).toBe(true)
  })

  it('says when yt-dlp is not installed yet', async () => {
    http.mockReturnValueOnce(reply({ version: '' }))
    render(<YtDlpControl />)

    expect(
      await screen.findByText('resources.rip.ytdlp.notInstalled'),
    ).toBeInTheDocument()
  })

  it("says why the version can't be shown", async () => {
    http.mockImplementationOnce(() =>
      Promise.reject({
        message: 'Internal Server Error',
        body: { error: "yt-dlp doesn't run: exit status 1" },
      }),
    )
    render(<YtDlpControl />)

    expect(
      await screen.findByText(
        /resources.rip.ytdlp.unknown.*yt-dlp doesn't run: exit status 1/,
      ),
    ).toBeInTheDocument()
  })

  it('updates yt-dlp on demand and shows the new version', async () => {
    http
      .mockReturnValueOnce(
        reply({ version: '2026.10.01', latest: '2026.11.01' }),
      )
      .mockReturnValueOnce(
        reply({ previous: '2026.10.01', version: '2026.11.01', updated: true }),
      )
      .mockReturnValueOnce(
        reply({ version: '2026.11.01', latest: '2026.11.01' }),
      )
    render(<YtDlpControl />)
    await screen.findByText(/versionAvailable/)

    fireEvent.click(updateButton())

    expect(
      await screen.findByText(
        /resources.rip.ytdlp.versionUpToDate.*2026.11.01/,
      ),
    ).toBeInTheDocument()
    expect(highlighted(updateButton())).toBe(false)
    expect(http).toHaveBeenCalledWith('/api/rip/ytdlp/update', {
      method: 'POST',
    })
    expect(http).toHaveBeenLastCalledWith('/api/rip/ytdlp')
    expect(notify).toHaveBeenCalledWith(
      expect.stringContaining('resources.rip.ytdlp.updated'),
      'info',
    )
  })

  it('says when yt-dlp is already up to date', async () => {
    http
      .mockReturnValueOnce(reply({ version: '2026.11.01' }))
      .mockReturnValueOnce(
        reply({
          previous: '2026.11.01',
          version: '2026.11.01',
          updated: false,
        }),
      )
      .mockReturnValueOnce(reply({ version: '2026.11.01' }))
    render(<YtDlpControl />)
    await screen.findByText(/2026.11.01/)
    fireEvent.click(
      screen.getByRole('button', { name: /resources.rip.ytdlp.update/ }),
    )

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        expect.stringContaining('resources.rip.ytdlp.upToDate'),
        'info',
      ),
    )
  })

  it("shows the server's reason when the update fails, keeping the version shown", async () => {
    http
      .mockReturnValueOnce(reply({ version: '2026.10.01' }))
      .mockImplementationOnce(() =>
        Promise.reject({
          message: 'Conflict',
          body: {
            error: 'a Rip is running; update yt-dlp once it has finished',
          },
        }),
      )
    render(<YtDlpControl />)
    await screen.findByText(/2026.10.01/)
    fireEvent.click(
      screen.getByRole('button', { name: /resources.rip.ytdlp.update/ }),
    )

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'a Rip is running; update yt-dlp once it has finished',
        'warning',
      ),
    )
    expect(screen.getByText(/2026.10.01/)).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /resources.rip.ytdlp.update/ }),
    ).toBeEnabled()
  })

  it("shows the updated version even when the new state can't be read back", async () => {
    http
      .mockReturnValueOnce(
        reply({ version: '2026.10.01', latest: '2026.11.01' }),
      )
      .mockReturnValueOnce(
        reply({ previous: '2026.10.01', version: '2026.11.01', updated: true }),
      )
      .mockImplementationOnce(() => Promise.reject({ message: 'Bad Gateway' }))
    render(<YtDlpControl />)
    await screen.findByText(/versionAvailable/)

    fireEvent.click(updateButton())

    expect(
      await screen.findByText(/resources.rip.ytdlp.version .*2026.11.01/),
    ).toBeInTheDocument()
    expect(highlighted(updateButton())).toBe(false)
  })

  it('disables the button while updating', async () => {
    http
      .mockReturnValueOnce(reply({ version: '2026.10.01' }))
      .mockReturnValueOnce(new Promise(() => {}))
    render(<YtDlpControl />)
    await screen.findByText(/2026.10.01/)
    const button = screen.getByRole('button', {
      name: /resources.rip.ytdlp.update/,
    })
    fireEvent.click(button)

    await waitFor(() => expect(button).toBeDisabled())
  })
})
