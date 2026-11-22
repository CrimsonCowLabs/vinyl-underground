import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, createStore } from 'redux'
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import { ThemeProvider, useTheme } from '@material-ui/core/styles'
import { createMuiTheme } from 'react-admin'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { PlayerThemeProvider } from './PlayerThemeProvider'
import { themeReducer } from '../reducers/themeReducer'
import { changeAccent } from '../actions'
import { readCoverPixels } from '../themes/coverPixels'
import {
  VinylUndergroundDarkTheme,
  VinylUndergroundLightTheme,
} from '../themes/vinylUnderground'
import NordTheme from '../themes/nord'
import { contrastRatio } from '../themes/contrast'
import { surfaces } from '../themes/vinylUndergroundPalette'

// Reading a cover needs a real canvas, which jsdom lacks
vi.mock('../themes/coverPixels', () => ({ readCoverPixels: vi.fn() }))

const solid = ([r, g, b]) => {
  const data = new Uint8ClampedArray(32 * 32 * 4)
  for (let i = 0; i < data.length; i += 4) data.set([r, g, b, 255], i)
  return data
}
const covers = {
  'red.jpg': solid([200, 30, 30]),
  'blue.jpg': solid([30, 60, 200]),
  'grey.jpg': solid([128, 128, 128]),
  // the server's placeholder, for an album with no cover
  'placeholder.webp': null,
}

// What a component styled with the theme's palette keys sees
const Accent = ({ label }) => {
  const theme = useTheme()
  return <span data-testid={label}>{theme.palette.primary.main}</span>
}

// The app's theme around everything, as the layout sets it, and the
// player's subtree inside PlayerThemeProvider
const renderApp = (theme, cover, accent = 'art') => {
  const store = createStore(combineReducers({ theme: themeReducer }))
  store.dispatch(changeAccent(accent))
  const tree = (c) => (
    <Provider store={store}>
      <ThemeProvider theme={createMuiTheme(theme)}>
        <Accent label="app" />
        <PlayerThemeProvider theme={theme} cover={c}>
          <Accent label="player" />
        </PlayerThemeProvider>
      </ThemeProvider>
    </Provider>
  )
  const view = render(tree(cover))
  return { rerender: (c) => view.rerender(tree(c)) }
}

const accentOf = (label) => screen.getByTestId(label).textContent

describe('PlayerThemeProvider', () => {
  beforeEach(() => {
    readCoverPixels.mockImplementation((url) =>
      url in covers
        ? Promise.resolve(covers[url])
        : Promise.reject(new Error('failed to load')),
    )
  })
  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  describe('with From album art', () => {
    it('colours the player from the cover, readable on Slate, and leaves the rest of the app Teal', async () => {
      renderApp(VinylUndergroundDarkTheme, 'red.jpg')
      await waitFor(() => expect(accentOf('player')).toBe('#E76060'))
      expect(accentOf('app')).toBe('#3EC1B0')
    })

    it('darkens the cover colour to read on cream', async () => {
      renderApp(VinylUndergroundLightTheme, 'red.jpg')
      await waitFor(() => expect(accentOf('player')).toBe('#C41D1D'))
      expect(accentOf('app')).toBe('#086A5F')
    })

    it('styles the player bar with the cover colour', async () => {
      renderApp(VinylUndergroundDarkTheme, 'red.jpg')
      await waitFor(() => expect(accentOf('player')).toBe('#E76060'))
      expect(
        document.querySelector('style[data-vu-player-accent]').textContent,
      ).toContain('#E76060')
    })

    it('fades to the next Track’s cover', async () => {
      const app = renderApp(VinylUndergroundDarkTheme, 'red.jpg')
      await waitFor(() => expect(accentOf('player')).toBe('#E76060'))
      app.rerender('blue.jpg')
      await waitFor(() => expect(accentOf('player')).not.toBe('#E76060'))
      expect(accentOf('player')).not.toBe('#6D83E9')
      await waitFor(() => expect(accentOf('player')).toBe('#6D83E9'), {
        timeout: 2000,
      })
    })

    it('stays readable (AA) all through the fade', async () => {
      const app = renderApp(VinylUndergroundDarkTheme, 'red.jpg')
      await waitFor(() => expect(accentOf('player')).toBe('#E76060'))
      app.rerender('blue.jpg')
      const seen = new Set()
      await waitFor(
        () => {
          seen.add(accentOf('player'))
          expect(accentOf('player')).toBe('#6D83E9')
        },
        { timeout: 2000, interval: 10 },
      )
      expect(seen.size).toBeGreaterThan(2)
      seen.forEach((hex) => {
        expect(
          contrastRatio(hex, surfaces.dark.background),
        ).toBeGreaterThanOrEqual(4.5)
        expect(
          contrastRatio(hex, surfaces.dark.surface2),
        ).toBeGreaterThanOrEqual(4.5)
      })
    })

    it.each([
      ['a grey cover', 'grey.jpg'],
      ['a cover that fails to load', 'missing.jpg'],
      ['a missing cover (the server’s placeholder)', 'placeholder.webp'],
      ['no cover', undefined],
    ])('falls back to Teal for %s', async (_, cover) => {
      renderApp(VinylUndergroundDarkTheme, cover)
      if (cover) {
        await waitFor(() => expect(readCoverPixels).toHaveBeenCalled())
      }
      await new Promise((r) => setTimeout(r, 20))
      expect(accentOf('player')).toBe('#3EC1B0')
      expect(document.querySelector('style[data-vu-player-accent]')).toBeNull()
    })
  })

  it('passes a fixed accent through untouched', async () => {
    renderApp(VinylUndergroundDarkTheme, 'red.jpg', 'teal')
    await new Promise((r) => setTimeout(r, 20))
    expect(accentOf('player')).toBe('#3EC1B0')
    expect(readCoverPixels).not.toHaveBeenCalled()
  })

  it('leaves an inherited theme alone', async () => {
    renderApp(NordTheme, 'red.jpg')
    await new Promise((r) => setTimeout(r, 20))
    expect(accentOf('player')).toBe(NordTheme.palette.primary.main)
    expect(readCoverPixels).not.toHaveBeenCalled()
  })
})
