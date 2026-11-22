import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { RefreshMetadataButton } from './RefreshMetadataButton'

const mockNotify = vi.fn()
const mockRefreshMetadata = vi.fn()
const { mockPermissions } = vi.hoisted(() => ({
  mockPermissions: { value: 'admin' },
}))

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useNotify: () => mockNotify,
    useDataProvider: () => ({ refreshMetadata: mockRefreshMetadata }),
    usePermissions: () => ({ permissions: mockPermissions.value }),
    useTranslate: () => (x) => x,
  }
})

describe('RefreshMetadataButton', () => {
  const record = { id: 'al-1', name: 'Album' }

  beforeEach(() => {
    vi.clearAllMocks()
    mockPermissions.value = 'admin'
    mockRefreshMetadata.mockResolvedValue({ data: { id: 'al-1' } })
  })

  const renderButton = (props = {}) =>
    render(
      <ThemeProvider theme={createTheme()}>
        <RefreshMetadataButton resource="album" record={record} {...props} />
      </ThemeProvider>,
    )

  it('renders an icon-only button labelled by the refresh action', () => {
    renderButton()
    const button = screen.getByRole('button', {
      name: 'resources.album.actions.refresh',
    })
    expect(button).toBeInTheDocument()
    expect(button).toHaveTextContent('')
  })

  it('shows the label as a tooltip on hover', async () => {
    renderButton()
    fireEvent.mouseOver(
      screen.getByRole('button', {
        name: 'resources.album.actions.refresh',
      }),
    )
    await waitFor(() =>
      expect(screen.getByRole('tooltip')).toHaveTextContent(
        'resources.album.actions.refresh',
      ),
    )
  })

  it('renders nothing for non-admin users', () => {
    mockPermissions.value = 'regular'
    const { container } = renderButton()
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing without a record', () => {
    const { container } = renderButton({ record: undefined })
    expect(container).toBeEmptyDOMElement()
  })

  it('requests a refresh for the record and notifies once it is done', async () => {
    renderButton()
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() =>
      expect(mockRefreshMetadata).toHaveBeenCalledWith('album', 'al-1'),
    )
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('message.metadataRefreshed'),
    )
  })

  it('shows the refresh running, and blocks another click, until the scan is done', async () => {
    let finish
    mockRefreshMetadata.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    renderButton()
    const button = screen.getByRole('button', {
      name: 'resources.album.actions.refresh',
    })
    fireEvent.click(button)

    await waitFor(() => expect(button).toBeDisabled())
    expect(screen.getByRole('progressbar')).toBeInTheDocument()
    expect(mockNotify).not.toHaveBeenCalled()

    finish({ data: { id: 'al-1' } })

    await waitFor(() => expect(button).not.toBeDisabled())
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(mockRefreshMetadata).toHaveBeenCalledTimes(1)
  })

  it('says so when another scan is running', async () => {
    mockRefreshMetadata.mockRejectedValue({ status: 409 })
    renderButton()
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'message.metadataRefreshBusy',
        'warning',
      ),
    )
  })

  it('passes the artist resource through', async () => {
    renderButton({ resource: 'artist', record: { id: 'ar-1' } })
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() =>
      expect(mockRefreshMetadata).toHaveBeenCalledWith('artist', 'ar-1'),
    )
  })

  it('notifies a warning when the request fails', async () => {
    mockRefreshMetadata.mockRejectedValue(new Error('boom'))
    renderButton()
    fireEvent.click(screen.getByRole('button'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('ra.page.error', 'warning'),
    )
  })
})
