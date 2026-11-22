import React from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import TabBar from './TabBar'

let offline
let supported

vi.mock('react-admin', () => ({
  useTranslate: () => (key) => key,
}))

vi.mock('../download', () => ({
  downloadsSupported: () => supported,
  useIsOffline: () => offline,
  isAvailableOffline: (path) =>
    ['album', 'playlist', 'downloads'].includes(path.split('/')[1]),
}))

// The sheet has its own tests; here it only shows whether it is open
vi.mock('./MoreSheet', () => ({
  default: ({ open }) => (open ? <div role="dialog">more sheet</div> : null),
}))

let location
const renderAt = (path) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <TabBar />
      <Route
        path="*"
        render={({ location: l }) => {
          location = l
          return null
        }}
      />
    </MemoryRouter>,
  )

const tab = (name) => screen.getByRole('link', { name: `menu.tabBar.${name}` })
const tabNames = () =>
  screen
    .getAllByRole('link')
    .map((el) => el.textContent)
    .concat(screen.getAllByRole('button').map((el) => el.textContent))
const current = () =>
  screen
    .getByRole('navigation')
    .querySelector('[aria-current]')
    ?.textContent.trim()

describe('<TabBar />, the main navigation on phones', () => {
  beforeEach(() => {
    offline = false
    supported = true
    localStorage.clear()
  })
  afterEach(cleanup)

  it('is a navigation landmark with five labelled tabs', () => {
    renderAt('/album/recentlyAdded')
    expect(
      screen.getByRole('navigation', { name: 'menu.tabBar.label' }),
    ).toBeInTheDocument()
    expect(tabNames()).toEqual([
      'menu.tabBar.library',
      'menu.tabBar.playlists',
      'menu.tabBar.stations',
      'menu.tabBar.downloads',
      'menu.tabBar.more',
    ])
  })

  it('leaves out Downloads where the browser cannot store them', () => {
    supported = false
    renderAt('/album/recentlyAdded')
    expect(tabNames()).toEqual([
      'menu.tabBar.library',
      'menu.tabBar.playlists',
      'menu.tabBar.stations',
      'menu.tabBar.more',
    ])
  })

  it.each([
    ['/', 'menu.tabBar.library'],
    ['/album/recentlyAdded', 'menu.tabBar.library'],
    ['/album/a1/show', 'menu.tabBar.library'],
    ['/artist', 'menu.tabBar.library'],
    ['/song', 'menu.tabBar.library'],
    ['/playlist', 'menu.tabBar.playlists'],
    ['/playlist/p1/show', 'menu.tabBar.playlists'],
    ['/station', 'menu.tabBar.stations'],
    ['/downloads', 'menu.tabBar.downloads'],
    ['/radio', 'menu.tabBar.more'],
    ['/rip', 'menu.tabBar.more'],
    ['/personal', 'menu.tabBar.more'],
    ['/user', 'menu.tabBar.more'],
    ['/correction', 'menu.tabBar.more'],
  ])('highlights the section of %s', (path, name) => {
    renderAt(path)
    expect(current()).toBe(name)
  })

  it.each([
    ['playlists', '/playlist'],
    ['stations', '/station'],
    ['downloads', '/downloads'],
  ])('opens %s', (name, path) => {
    renderAt('/album/recentlyAdded')
    fireEvent.click(tab(name))
    expect(location.pathname).toBe(path)
  })

  it('opens the default album list from Library', () => {
    localStorage.setItem('defaultView', 'mostPlayed')
    renderAt('/playlist')
    fireEvent.click(tab('library'))
    expect(location.pathname).toBe('/album/mostPlayed')
  })

  it('opens albums from Library even when the default view is another page', () => {
    localStorage.setItem('defaultView', 'playlist')
    renderAt('/station')
    fireEvent.click(tab('library'))
    expect(location.pathname).toBe('/album/recentlyAdded')
  })

  it('opens the More sheet, keeping the page current while it is open', () => {
    renderAt('/album/recentlyAdded')
    const more = screen.getByRole('button', { name: 'menu.tabBar.more' })
    expect(more).toHaveAttribute('aria-haspopup', 'dialog')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    fireEvent.click(more)

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(more).toHaveAttribute('aria-expanded', 'true')
    expect(more.className).toMatch(/current/)
    expect(current()).toBe('menu.tabBar.library')
  })

  describe('offline', () => {
    beforeEach(() => {
      offline = true
    })

    it('greys out Stations and says why', () => {
      renderAt('/album/recentlyAdded')
      expect(tab('stations')).toHaveAccessibleDescription(
        'download.offline.unavailableTitle',
      )
      expect(tab('stations').className).toMatch(/unavailable/)
    })

    it('keeps Library, Playlists, Downloads and More usable', () => {
      renderAt('/album/recentlyAdded')
      for (const name of ['library', 'playlists', 'downloads']) {
        expect(tab(name)).not.toHaveAccessibleDescription()
        expect(tab(name).className).not.toMatch(/unavailable/)
      }
      fireEvent.click(tab('downloads'))
      expect(location.pathname).toBe('/downloads')
      fireEvent.click(screen.getByRole('button', { name: 'menu.tabBar.more' }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })
})
