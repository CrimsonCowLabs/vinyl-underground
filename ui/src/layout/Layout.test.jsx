import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { act, cleanup, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { playerReducer } from '../reducers'
import { playTracks } from '../actions'
import {
  closeNowPlaying,
  nowPlayingReducer,
  openNowPlaying,
} from '../nowPlaying/nowPlayingState'
import Layout from './Layout'

// react-admin's layout stands in as the page: the sidebar and the content
vi.mock('react-admin', () => ({
  Layout: ({ className, children, sidebar: Sidebar }) => (
    <div data-testid="page" className={className}>
      <Sidebar>the menu</Sidebar>
      {children}
    </div>
  ),
  Sidebar: ({ children }) => <aside>{children}</aside>,
  toggleSidebar: () => ({ type: 'TOGGLE_SIDEBAR' }),
}))
vi.mock('./TabBar', () => ({
  default: () => <nav aria-label="tab bar" />,
}))
vi.mock('./Menu', () => ({ default: () => null }))
vi.mock('./AppBar', () => ({ default: () => null }))
vi.mock('./Notification', () => ({ default: () => null }))
vi.mock('../themes/useCurrentTheme', () => ({ default: () => ({}) }))
vi.mock('../common', async (importOriginal) => ({
  ...(await importOriginal()),
  useSearchRefocus: () => {},
}))
vi.mock('../download', async (importOriginal) => ({
  ...(await importOriginal()),
  useResumeDownloads: () => {},
}))
vi.mock('../download/OfflineGate', () => ({
  OfflineGate: ({ children }) => children,
}))

let width
const screenWidth = (w) => {
  width = w
  window.matchMedia = (query) => ({
    media: query,
    matches: width >= Number(/min-width:\s*(\d+)px/.exec(query)[1]),
    addListener: () => {},
    removeListener: () => {},
  })
}

const setup = () => {
  const store = createStore(
    combineReducers({ player: playerReducer, nowPlaying: nowPlayingReducer }),
  )
  store.dispatch(playTracks({ t1: { id: 't1', title: 'Lamplight Freeway' } }))
  render(
    <Provider store={store}>
      <Layout>content</Layout>
    </Provider>,
  )
  return {
    dispatch: (action) =>
      act(() => {
        store.dispatch(action)
      }),
  }
}

// The page's right padding in px, from the style rules matching it (jsdom's
// getComputedStyle misses JSS updating a rule in place)
const pageRoomOnTheRight = () => {
  const page = screen.getByTestId('page')
  const rules = [...document.styleSheets].flatMap((sheet) => [
    ...sheet.cssRules,
  ])
  return rules
    .filter((rule) => rule.selectorText && page.matches(rule.selectorText))
    .map((rule) => rule.style.getPropertyValue('padding-right'))
    .filter(Boolean)
    .map(parseFloat)
    .pop()
}

describe('Layout beside the Now Playing panel', () => {
  beforeEach(() => screenWidth(1440))
  afterEach(() => {
    cleanup()
    delete window.matchMedia
  })

  it('moves the page over for the panel, rather than under it', () => {
    const { dispatch } = setup()
    expect(pageRoomOnTheRight()).toBe(0)

    dispatch(openNowPlaying())
    expect(pageRoomOnTheRight()).toBe(400)

    dispatch(closeNowPlaying())
    expect(pageRoomOnTheRight()).toBe(0)
  })

  it('leaves the page alone below 1000px, where Now Playing is full screen', () => {
    screenWidth(999)
    const { dispatch } = setup()

    dispatch(openNowPlaying())

    expect(pageRoomOnTheRight()).toBe(0)
  })
})

describe('Layout on a phone and on desktop', () => {
  afterEach(() => {
    cleanup()
    delete window.matchMedia
  })

  it('navigates with the tab bar on a phone, without the side drawer', () => {
    screenWidth(390)
    setup()
    expect(
      screen.getByRole('navigation', { name: 'tab bar' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('the menu')).not.toBeInTheDocument()
  })

  it('keeps the side drawer on desktop, with no tab bar', () => {
    screenWidth(1440)
    setup()
    expect(screen.getByText('the menu')).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  })

  it('keeps the side drawer from 810px', () => {
    screenWidth(810)
    setup()
    expect(screen.getByText('the menu')).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  })
})
