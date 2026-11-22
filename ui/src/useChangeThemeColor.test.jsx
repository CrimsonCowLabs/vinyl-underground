import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import mediaQuery from 'css-mediaquery'
import { renderHook } from '@testing-library/react-hooks'
import useChangeThemeColor from './useChangeThemeColor'
import { themeReducer } from './reducers/themeReducer'

const prefer = (scheme) => {
  window.matchMedia = (query) => ({
    matches: mediaQuery.match(query, { 'prefers-color-scheme': scheme }),
    addListener: () => {},
    removeListener: () => {},
  })
}

const themeColorFor = (persisted) => {
  const store = createStore(
    combineReducers({ theme: themeReducer }),
    persisted && { theme: persisted },
  )
  renderHook(() => useChangeThemeColor(), {
    wrapper: ({ children }) => <Provider store={store}>{children}</Provider>,
  })
  return document
    .querySelector("meta[name='theme-color']")
    .getAttribute('content')
}

describe('useChangeThemeColor', () => {
  beforeEach(() => {
    document.head.innerHTML = '<meta name="theme-color" content="#ffffff">'
  })

  it('matches the Slate background on a new device in dark mode', () => {
    prefer('dark')
    expect(themeColorFor(undefined)).toBe('#0F1216')
  })

  it('matches the cream background on a new device in light mode', () => {
    prefer('light')
    expect(themeColorFor(undefined)).toBe('#F3EDE4')
  })

  it("keeps an inherited theme's colour", () => {
    prefer('dark')
    // Spotify-ish sets primary.light
    expect(themeColorFor({ choice: 'SpotifyTheme' })).toBe('#62ec83')
  })
})
