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

describe('YtDlpControl', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the installed version', async () => {
    http.mockReturnValueOnce(reply({ version: '2026.10.01' }))
    render(<YtDlpControl />)

    expect(
      await screen.findByText(/resources.rip.ytdlp.version.*2026.10.01/),
    ).toBeInTheDocument()
    expect(http).toHaveBeenCalledWith('/api/rip/ytdlp')
  })

  it('says when yt-dlp is not installed yet', async () => {
    http.mockReturnValueOnce(reply({ version: '' }))
    render(<YtDlpControl />)

    expect(
      await screen.findByText('resources.rip.ytdlp.notInstalled'),
    ).toBeInTheDocument()
  })

  it('updates yt-dlp on demand and shows the new version', async () => {
    http
      .mockReturnValueOnce(reply({ version: '2026.10.01' }))
      .mockReturnValueOnce(
        reply({ previous: '2026.10.01', version: '2026.11.01', updated: true }),
      )
    render(<YtDlpControl />)
    await screen.findByText(/2026.10.01/)

    fireEvent.click(
      screen.getByRole('button', { name: /resources.rip.ytdlp.update/ }),
    )

    expect(await screen.findByText(/2026.11.01/)).toBeInTheDocument()
    expect(http).toHaveBeenLastCalledWith('/api/rip/ytdlp/update', {
      method: 'POST',
    })
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
