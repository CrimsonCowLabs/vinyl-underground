import React from 'react'
import { render, screen, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import AlbumShow from './AlbumShow'

const { mockShow, mockRedirect, mockNotify } = vi.hoisted(() => ({
  mockShow: { props: null, record: undefined },
  mockRedirect: vi.fn(),
  mockNotify: vi.fn(),
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useShowController: (props) => {
    mockShow.props = props
    return { record: mockShow.record, loading: false, loaded: true }
  },
  useRedirect: () => mockRedirect,
  useNotify: () => mockNotify,
  useTranslate: () => (key) => key,
  ReferenceManyField: () => <div data-testid="songs" />,
  Title: () => null,
}))
vi.mock('./AlbumDetails', () => ({ default: () => <div>details</div> }))
vi.mock('./AlbumSongs', () => ({ default: () => null }))
vi.mock('./AlbumActions', () => ({ default: () => null }))
vi.mock('../common', () => ({
  useResourceRefresh: () => {},
  useScrollRestoration: () => {},
  Title: () => null,
}))

// An edit that changes an album's title or album artist gives it a new ID: once its job's rescan
// is done the old album is gone, and the open page reloads it. The page stays and says so,
// instead of react-admin's sending the admin to the album list.
describe('AlbumShow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockShow.record = { id: 'al1', name: 'Record' }
  })

  it('stays, saying the album has changed, when it is no longer there', () => {
    render(<AlbumShow id="al1" resource="album" basePath="/album" />)
    expect(screen.getByText('details')).toBeTruthy()

    act(() => mockShow.props.onFailure(new Error('not found')))

    expect(mockRedirect).not.toHaveBeenCalled()
    expect(screen.getByText('resources.album.gone')).toBeTruthy()
    expect(screen.queryByText('details')).toBeNull()
  })

  it('shows the album until then', () => {
    render(<AlbumShow id="al1" resource="album" basePath="/album" />)
    expect(screen.getByText('details')).toBeTruthy()
    expect(screen.queryByText('resources.album.gone')).toBeNull()
  })
})
