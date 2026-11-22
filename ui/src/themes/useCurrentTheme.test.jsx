import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import mediaQuery from 'css-mediaquery'
import { renderHook } from '@testing-library/react-hooks'
import useCurrentTheme from './useCurrentTheme'
import { themeReducer } from '../reducers/themeReducer'
import { changeTheme } from '../actions'
import { AUTO_THEME_ID } from '../consts'

function createMatchMedia(theme) {
  return (query) => ({
    matches: mediaQuery.match(query, { 'prefers-color-scheme': theme }),
    addListener: () => {},
    removeListener: () => {},
  })
}

// `persisted` is what localStorage held for the theme, if anything
const renderWith = (persisted, ...actions) => {
  const store = createStore(
    combineReducers({ theme: themeReducer }),
    persisted === undefined ? undefined : { theme: persisted },
  )
  actions.forEach((action) => store.dispatch(action))
  return renderHook(() => useCurrentTheme(), {
    wrapper: ({ children }) => <Provider store={store}>{children}</Provider>,
  }).result.current
}

beforeEach(() => {
  document.body.style.backgroundColor = ''
})

describe('useCurrentTheme', () => {
  describe.each([
    ['light', 'Vinyl Underground Light'],
    ['dark', 'Vinyl Underground Dark'],
  ])('when the system prefers %s', (scheme, vinylUnderground) => {
    beforeAll(() => {
      window.matchMedia = createMatchMedia(scheme)
    })

    it(`shows ${vinylUnderground} on a new device`, () => {
      expect(renderWith(undefined).themeName).toBe(vinylUnderground)
    })

    it(`shows ${vinylUnderground} for Auto`, () => {
      expect(renderWith({ choice: AUTO_THEME_ID }).themeName).toBe(
        vinylUnderground,
      )
      expect(renderWith(undefined, changeTheme(AUTO_THEME_ID)).themeName).toBe(
        vinylUnderground,
      )
    })

    it(`shows ${vinylUnderground} for a device that kept the old default`, () => {
      expect(renderWith('DarkTheme').themeName).toBe(vinylUnderground)
    })

    it.each([
      ['DarkTheme', 'Dark'],
      ['LightTheme', 'Light'],
      ['SpotifyTheme', 'Spotify-ish'],
      ['VinylUndergroundLightTheme', 'Vinyl Underground Light'],
      ['VinylUndergroundDarkTheme', 'Vinyl Underground Dark'],
    ])('shows %s when chosen', (id, name) => {
      expect(renderWith(undefined, changeTheme(id)).themeName).toBe(name)
      expect(renderWith({ choice: id }).themeName).toBe(name)
    })

    it.each([
      ['LightTheme', 'Light'],
      ['SpotifyTheme', 'Spotify-ish'],
      ['NordTheme', 'Nord'],
    ])('keeps %s, chosen before the Vinyl Underground theme', (id, name) => {
      expect(renderWith(id).themeName).toBe(name)
    })
  })

  describe('body background color', () => {
    beforeAll(() => {
      window.matchMedia = createMatchMedia('dark')
    })
    it('sets body background for dark theme', () => {
      renderWith({ choice: 'DarkTheme' })
      // Dark theme uses MUI default dark background
      expect(document.body.style.backgroundColor).toBe('rgb(48, 48, 48)')
    })
    it('sets body background for light theme', () => {
      renderWith({ choice: 'LightTheme' })
      // Light theme uses MUI default light background
      expect(document.body.style.backgroundColor).toBe('rgb(250, 250, 250)')
    })
    it('sets body background for theme with custom background', () => {
      renderWith({ choice: 'SpotifyTheme' })
      // Spotify theme has explicit background.default: #121212
      expect(document.body.style.backgroundColor).toBe('rgb(18, 18, 18)')
    })
    it('sets the Slate background for the Vinyl Underground theme', () => {
      renderWith(undefined)
      expect(document.body.style.backgroundColor).toBe('rgb(15, 18, 22)')
    })
  })
})
