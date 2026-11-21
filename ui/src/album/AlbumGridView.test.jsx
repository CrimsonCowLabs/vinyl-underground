import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route } from 'react-router-dom'
import { ThemeProvider, createMuiTheme } from '@material-ui/core/styles'
import { describe, it, expect, vi } from 'vitest'
import AlbumGridView from './AlbumGridView'

// react-admin's Link/useListContext need a router and a store; the grid body is not under test here.
vi.mock('react-admin', () => ({
  linkToRecord: (basePath, id) => `${basePath}/${id}/show`,
  useListContext: () => ({}),
  useTranslate: () => (key) => key,
  Loading: () => <div data-testid="loading" />,
}))

// The tile's controls need the player and a store; only their place matters here
vi.mock('../common', () => ({
  AlbumContextMenu: () => <button>context menu</button>,
  PlayButton: () => <button>play</button>,
  ArtistLinkField: () => null,
  OverflowTooltip: ({ children }) => children,
}))
vi.mock('../common/Artwork', () => ({
  Artwork: ({ title }) => <img alt={title} />,
}))
vi.mock('react-dnd', () => ({ useDrag: () => [{}, () => {}] }))

const downloadOf = vi.hoisted(() => ({}))
vi.mock('../download/index', () => ({
  downloadsSupported: () => true,
  useDownload: (id, kind) => (kind === 'album' ? downloadOf[id] : undefined),
}))

const albums = {
  al1: { id: 'al1', name: 'Blue' },
  al2: { id: 'al2', name: 'Hejira' },
}

const renderGrid = () =>
  render(
    <ThemeProvider theme={createMuiTheme()}>
      <MemoryRouter>
        <AlbumGridView
          data={albums}
          ids={['al1', 'al2']}
          basePath="/album"
          width="md"
        />
        <Route
          path="*"
          render={({ location }) => (
            <div data-testid="location">{location.pathname}</div>
          )}
        />
      </MemoryRouter>
    </ThemeProvider>,
  )

describe('AlbumGridView', () => {
  // ArtistShow renders the grid through ReferenceManyField, which passes no seed tracking.
  it('renders without a shownSeed ref', () => {
    expect(() =>
      render(<AlbumGridView data={{}} ids={[]} basePath="/album" width="md" />),
    ).not.toThrow()
  })

  it("shows a Download badge on the cover of an album that has one, and only that album's", () => {
    downloadOf.al1 = { id: 'al1', kind: 'album', state: 'downloaded' }

    renderGrid()

    const badges = screen.getAllByRole('img', {
      name: 'download.badge.downloaded',
    })
    expect(badges).toHaveLength(1)
    expect(badges[0].closest('a')).toHaveAttribute('href', '/album/al1/show')
  })

  it('still opens the album when its badge is clicked', () => {
    downloadOf.al1 = { id: 'al1', kind: 'album', state: 'interrupted' }
    renderGrid()

    fireEvent.click(
      screen.getByRole('img', { name: 'download.badge.interrupted.album' }),
    )

    expect(screen.getByTestId('location')).toHaveTextContent('/album/al1/show')
  })
})
