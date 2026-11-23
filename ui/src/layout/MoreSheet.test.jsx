import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MoreSheet from './MoreSheet'

// The album lists read it when loaded
const config = vi.hoisted(() => ({
  enableFavourites: true,
  enableStarRating: true,
}))
let permissions
let offline
let editingEnabled = false

vi.mock('../config', () => ({ default: config }))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => (key, options) =>
    options?.smart_count === 1 ? `${key} (one)` : key,
  usePermissions: () => ({ permissions }),
}))

vi.mock('../download', () => ({
  useIsOffline: () => offline,
  isAvailableOffline: (path) =>
    ['album', 'playlist', 'personal', 'downloads'].includes(path.split('/')[1]),
}))

vi.mock('../common/LibrarySelector', () => ({ default: () => null }))

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => ({ loaded: true, enabled: editingEnabled }),
}))

vi.mock('../playlist/GeneratePlaylistButton', () => ({
  GeneratePlaylistDialog: ({ open }) =>
    open ? <div>generate dialog</div> : null,
}))

// The resources as App.jsx registers them for each role, with the config
// below (sharing on, Inbox on, no plugins)
const listed = (name, options = {}) => ({
  [name]: { props: { name, hasList: true, options } },
})
const unlisted = (name) => ({
  [name]: { props: { name, hasList: false, options: {} } },
})
const settings = { subMenu: 'settings' }
const RESOURCES = {
  listener: {
    ...listed('album', { subMenu: 'albumList' }),
    ...listed('artist'),
    ...listed('song'),
    ...listed('station'),
    ...listed('radio'),
    ...listed('share'),
    ...listed('playlist', { subMenu: 'playlist' }),
    ...listed('user', settings),
    ...listed('player', settings),
    ...unlisted('transcoding'),
    ...unlisted('genre'),
  },
  admin: {
    ...listed('album', { subMenu: 'albumList' }),
    ...listed('artist'),
    ...listed('song'),
    ...listed('station'),
    ...listed('radio'),
    ...listed('share'),
    ...listed('rip'),
    ...listed('playlist', { subMenu: 'playlist' }),
    ...listed('user', settings),
    ...listed('player', settings),
    ...listed('transcoding', settings),
    ...listed('library', settings),
    ...listed('missing', settings),
    ...listed('correction', settings),
    ...listed('libraryTrack', { subMenu: 'settings', editing: true }),
    ...unlisted('genre'),
  },
}

let location
const renderSheet = (role, { onClose = () => {} } = {}) => {
  permissions = role
  const store = createStore(
    combineReducers({
      admin: () => ({ resources: RESOURCES[role] }),
      library: () => ({ userLibraries: [], selectedLibraries: [] }),
    }),
  )
  render(
    <Provider store={store}>
      <MemoryRouter initialEntries={['/album/recentlyAdded']}>
        <MoreSheet open onClose={onClose} />
        <Route
          path="*"
          render={({ location: l }) => {
            location = l
            return null
          }}
        />
      </MemoryRouter>
    </Provider>,
  )
}

const item = (name) => screen.queryByRole('link', { name })
const shown = (names) => names.filter((name) => item(name))

const LIBRARY = [
  'resources.artist.name',
  'resources.song.name',
  'resources.radio.name',
  'resources.share.name',
]
const ADMIN_PAGES = [
  'resources.rip.name',
  'resources.user.name',
  'resources.transcoding.name',
  'resources.library.name',
  'resources.missing.name',
  'resources.correction.name',
]

describe('<MoreSheet />, what the tab bar leaves out', () => {
  beforeEach(() => {
    Object.assign(config, {
      enableUserEditing: true,
      enableGeneratedPlaylists: true,
    })
    offline = false
    localStorage.setItem('userId', 'u1')
  })
  afterEach(cleanup)

  it('lists the Library manager only while editing is on', () => {
    editingEnabled = false
    renderSheet('admin')
    expect(item('resources.libraryTrack.name')).not.toBeInTheDocument()
    cleanup()
    editingEnabled = true
    renderSheet('admin')
    expect(item('resources.libraryTrack.name')).toHaveAttribute(
      'href',
      '/libraryTrack',
    )
    editingEnabled = false
  })

  it('is a sheet titled More', () => {
    renderSheet('listener')
    expect(
      screen.getByRole('dialog', { name: 'menu.tabBar.more' }),
    ).toBeInTheDocument()
  })

  it("holds the drawer's album lists, which no tab opens", () => {
    renderSheet('listener')
    expect(item('resources.album.lists.all')).toHaveAttribute(
      'href',
      '/album/all',
    )
    expect(item('resources.album.lists.random')).toBeInTheDocument()
    expect(item('resources.album.lists.starred')).toBeInTheDocument()
  })

  it("holds a Listener's library pages, personal settings and own profile", () => {
    renderSheet('listener')
    expect(shown(LIBRARY)).toEqual(LIBRARY)
    expect(item('menu.personal.name')).toHaveAttribute('href', '/personal')
    expect(item('resources.user.name (one)')).toHaveAttribute(
      'href',
      '/user/u1',
    )
    expect(item('resources.player.name')).toHaveAttribute('href', '/player')
  })

  it('hides the admin pages from a Listener', () => {
    renderSheet('listener')
    expect(shown(ADMIN_PAGES)).toEqual([])
  })

  it("hides a Listener's profile when user editing is off", () => {
    config.enableUserEditing = false
    renderSheet('listener')
    expect(item('resources.user.name (one)')).not.toBeInTheDocument()
  })

  it('holds the Rips, Inbox and admin pages for an admin', () => {
    renderSheet('admin')
    expect(shown(ADMIN_PAGES)).toEqual(ADMIN_PAGES)
    expect(item('resources.rip.name')).toHaveAttribute('href', '/rip')
  })

  it('leaves out what the tabs open', () => {
    renderSheet('admin')
    expect(item('resources.station.name')).not.toBeInTheDocument()
    expect(item('resources.playlist.name')).not.toBeInTheDocument()
  })

  it('opens Generate a Playlist when the server has an LLM', () => {
    renderSheet('listener')
    fireEvent.click(
      screen.getByRole('button', { name: 'menu.more.generatePlaylist' }),
    )
    expect(screen.getByText('generate dialog')).toBeInTheDocument()
  })

  it('leaves out Generate a Playlist without an LLM', () => {
    config.enableGeneratedPlaylists = false
    renderSheet('listener')
    expect(
      screen.queryByRole('button', { name: 'menu.more.generatePlaylist' }),
    ).not.toBeInTheDocument()
  })

  it('opens the page picked, and closes', () => {
    const onClose = vi.fn()
    renderSheet('listener', { onClose })
    fireEvent.click(item('resources.artist.name'))
    expect(location.pathname).toBe('/artist')
    expect(onClose).toHaveBeenCalled()
  })

  describe('offline', () => {
    beforeEach(() => {
      offline = true
    })

    it('greys out what needs the server, as the drawer does', () => {
      renderSheet('admin')
      for (const name of [...LIBRARY, ...ADMIN_PAGES]) {
        expect(item(name)).not.toBeInTheDocument()
        const disabled = screen.getByText(name).closest('[aria-disabled]')
        expect(disabled).toHaveAttribute('aria-disabled', 'true')
        expect(disabled).toHaveAccessibleDescription(
          'download.offline.unavailableTitle',
        )
      }
      const generate = screen.getByRole('button', {
        name: 'menu.more.generatePlaylist',
      })
      expect(generate).toBeDisabled()
      expect(generate).toHaveAccessibleDescription(
        'download.offline.unavailableTitle',
      )
    })

    it('keeps the album lists and personal settings', () => {
      renderSheet('listener')
      expect(item('resources.album.lists.all')).toBeInTheDocument()
      expect(item('menu.personal.name')).toBeInTheDocument()
    })
  })
})
