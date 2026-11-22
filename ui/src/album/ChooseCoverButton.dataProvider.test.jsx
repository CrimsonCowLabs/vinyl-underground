import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { DataProviderContext } from 'react-admin'
import { TestContext } from 'ra-test'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import dataProvider from '../dataProvider/wrapperDataProvider'
import { ChooseCoverButton } from './ChooseCoverButton'

// The picker through react-admin's real useDataProvider and the app's data provider, down to
// fetch: what the server is asked, and how.

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  usePermissions: () => ({ permissions: 'admin' }),
}))

vi.mock('../subsonic', () => ({
  default: { getCoverArtUrl: () => '/cover' },
}))

const candidates = {
  candidates: [{ id: 'c-folder', source: 'folder', label: 'cover.jpg' }],
  failed: false,
}

// The server: the album folder's candidates (and none elsewhere) as JSON, previews as PNGs
const server = (url, { signal } = {}) => {
  if (url.includes('/preview')) {
    server.previewSignals.push(signal)
    return Promise.resolve(
      new Response(new Blob(['png'], { type: 'image/png' }), {
        headers: { 'Content-Type': 'image/png' },
      }),
    )
  }
  return Promise.resolve(
    new Response(
      JSON.stringify(
        url.includes('source=local')
          ? candidates
          : { candidates: [], failed: false },
      ),
      {
        headers: { 'Content-Type': 'application/json' },
      },
    ),
  )
}

describe('ChooseCoverButton, through the data provider', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.setItem('token', 'secret-token')
    server.previewSignals = []
    fetchMock = vi.fn(server)
    vi.stubGlobal('fetch', fetchMock)
    URL.createObjectURL = vi.fn(() => 'blob:preview')
    URL.revokeObjectURL = vi.fn()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.removeItem('token')
  })

  it('fetches each preview with the Authorization header, shows it from a blob URL, and releases it on close', async () => {
    render(
      <DataProviderContext.Provider value={dataProvider}>
        <ThemeProvider theme={createTheme()}>
          <TestContext>
            <ChooseCoverButton record={{ id: 'al1', name: 'Record' }} />
          </TestContext>
        </ThemeProvider>
      </DataProviderContext.Provider>,
    )
    fireEvent.click(screen.getByText('resources.album.actions.chooseCover'))

    const img = await screen.findByAltText('cover.jpg')
    expect(img.getAttribute('src')).toBe('blob:preview')

    const previewCall = fetchMock.mock.calls.find(([url]) =>
      url.includes('/preview'),
    )
    expect(previewCall[0]).toBe(
      '/api/albumCover/al1/candidates/c-folder/preview',
    )
    expect(previewCall[1].headers.get('X-ND-Authorization')).toBe(
      'Bearer secret-token',
    )
    for (const [url] of fetchMock.mock.calls) {
      expect(url).not.toContain('secret-token')
    }
    // The signal reaches fetch, so closing can cancel a preview under way
    expect(server.previewSignals[0]).toBeInstanceOf(AbortSignal)

    fireEvent.click(screen.getByText('ra.action.cancel'))
    await waitFor(() =>
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview'),
    )
    expect(server.previewSignals[0].aborted).toBe(true)
  })
})
