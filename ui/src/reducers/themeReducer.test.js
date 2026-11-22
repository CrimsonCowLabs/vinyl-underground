import { describe, it, expect, vi, beforeEach } from 'vitest'
import { AUTO_THEME_ID, AUTO_THEME_CONFIG_VALUE } from '../consts'
import { CHANGE_THEME } from '../actions'

const loadWithConfig = async (defaultTheme) => {
  vi.doMock('../config', () => ({ default: { defaultTheme } }))
  return import('./themeReducer')
}

describe('themeReducer', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts with no choice on a new device', async () => {
    const { themeReducer } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
    expect(themeReducer(undefined, { type: '@@INIT' })).toEqual({
      choice: null,
    })
  })

  it.each([
    AUTO_THEME_ID,
    'NordTheme',
    'DarkTheme',
    'VinylUndergroundLightTheme',
  ])('records choosing %s', async (id) => {
    const { themeReducer } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
    const state = themeReducer(undefined, { type: '@@INIT' })
    expect(themeReducer(state, { type: CHANGE_THEME, payload: id })).toEqual({
      choice: id,
    })
  })

  it('keeps a choice across a reload', async () => {
    const { themeReducer } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
    const persisted = JSON.parse(JSON.stringify({ choice: 'DarkTheme' }))
    expect(themeReducer(persisted, { type: '@@INIT' })).toEqual({
      choice: 'DarkTheme',
    })
  })

  describe('migrating a theme saved before the Vinyl Underground theme', () => {
    // Before, the id itself was saved, and it was saved even when the
    // Listener never chose one, so the old default can't be told apart.
    it('treats the old default, Dark, as never chosen', async () => {
      const { themeReducer } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
      expect(themeReducer('DarkTheme', { type: '@@INIT' })).toEqual({
        choice: null,
      })
    })

    it.each([AUTO_THEME_ID, 'LightTheme', 'NordTheme', 'SpotifyTheme'])(
      'keeps %s, which the Listener chose',
      async (id) => {
        const { themeReducer } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
        expect(themeReducer(id, { type: '@@INIT' })).toEqual({ choice: id })
      },
    )
  })
})

describe('currentThemeId', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('is the choice when there is one', async () => {
    const { currentThemeId } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
    expect(currentThemeId({ theme: { choice: 'NordTheme' } })).toBe('NordTheme')
  })

  it.each([
    { configTheme: undefined, expected: AUTO_THEME_ID, why: 'is not set' },
    {
      configTheme: AUTO_THEME_CONFIG_VALUE,
      expected: AUTO_THEME_ID,
      why: 'is "Auto"',
    },
    { configTheme: 'Dark', expected: 'DarkTheme', why: 'is "Dark"' },
    {
      configTheme: 'Vinyl Underground Light',
      expected: 'VinylUndergroundLightTheme',
      why: 'is "Vinyl Underground Light"',
    },
    {
      configTheme: 'NonExistent',
      expected: AUTO_THEME_ID,
      why: 'is unrecognized',
    },
  ])(
    'with no choice, is $expected when the defaultTheme config $why',
    async ({ configTheme, expected }) => {
      const { currentThemeId } = await loadWithConfig(configTheme)
      expect(currentThemeId({ theme: { choice: null } })).toBe(expected)
    },
  )

  it('falls back to the default for a theme that no longer exists', async () => {
    const { currentThemeId } = await loadWithConfig(AUTO_THEME_CONFIG_VALUE)
    expect(currentThemeId({ theme: { choice: 'GoneTheme' } })).toBe(
      AUTO_THEME_ID,
    )
  })
})
